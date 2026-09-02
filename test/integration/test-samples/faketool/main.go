// Command faketool is a configurable stand-in for the external CLIs the
// integration tests fake on PATH (aws, az, terraform, cdk, sam,
// aws_completer, rundll32). The shell-script fakes it replaces don't run on
// Windows; this binary does, so the same tests cover every OS.
//
// The binary is copied under the tool name it impersonates and reads its
// behavior from a JSON config file stored next to it at "<executable path> +
// .fakecfg" (see the fakeToolConfig mirror struct in faketool_test.go).
// Stdout/stderr lines support placeholders expanded against the invocation:
//
//	{argN}              the Nth argument (1-based, before Shift is applied)
//	{args}              all arguments after the first Shift ones, space-joined
//	{env:NAME}          the value of environment variable NAME
//	{env:NAME:-fallback} same, but "fallback" when NAME is unset or empty
//	{env:NAME-fallback}  same, but "fallback" only when NAME is unset — the
//	                     sh ${NAME-fallback} distinction between "removed from
//	                     the environment" and "present but empty"
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// argCase short-circuits the default behavior when the invocation's arguments
// start with Args — e.g. a `--version` probe or `providers schema -json`.
type argCase struct {
	Args []string `json:"args"`
	// Shift drops this many leading arguments before the case's {args} is
	// rendered (the top-level Shift does not apply inside cases).
	Shift    int      `json:"shift,omitempty"`
	Stdout   []string `json:"stdout,omitempty"`
	Stderr   []string `json:"stderr,omitempty"`
	ExitCode int      `json:"exitCode,omitempty"`
}

type config struct {
	// Cases are checked in order before anything else; the first whose Args
	// prefix-match the invocation prints its output and exits.
	Cases []argCase `json:"cases,omitempty"`
	// SleepSeconds delays the output, giving PTY tests time to observe
	// spinners.
	SleepSeconds int `json:"sleepSeconds,omitempty"`
	// Shift drops this many leading arguments before {args} is rendered,
	// mirroring `shift N` in the shell scripts this replaces.
	Shift  int      `json:"shift,omitempty"`
	Stdout []string `json:"stdout,omitempty"`
	Stderr []string `json:"stderr,omitempty"`
	// RecordFile, when set, receives RecordContent (placeholder-expanded) —
	// used by the fake browser openers to capture the URL they were handed.
	RecordFile    string `json:"recordFile,omitempty"`
	RecordContent string `json:"recordContent,omitempty"`
	// DumpFile, when set and present in the working directory, is printed
	// line by line after Stdout, each line prefixed with DumpPrefix.
	DumpFile   string `json:"dumpFile,omitempty"`
	DumpPrefix string `json:"dumpPrefix,omitempty"`
	// Pager, when true, switches to pager mode after Stdout (the "first
	// page") is printed: single keystrokes are read raw from the terminal on
	// stderr and reported as "PAGER_GOT:<key>" lines, quitting on "q" —
	// modeling how less 577+ resolves its keyboard (the terminal named by
	// ttyname(2), falling back to /dev/tty, then reading fd 2 itself), which
	// under `lstk aws`/`lstk az` is lstk's inner PTY (DEVX-1049).
	Pager    bool `json:"pager,omitempty"`
	ExitCode int  `json:"exitCode,omitempty"`
}

var placeholder = regexp.MustCompile(`\{arg(\d+)\}|\{args\}|\{env:([A-Za-z0-9_]+)((:?-)([^}]*))?\}`)

func expand(line string, args, shifted []string) string {
	return placeholder.ReplaceAllStringFunc(line, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		switch {
		case sub[1] != "":
			n, _ := strconv.Atoi(sub[1])
			if n >= 1 && n <= len(args) {
				return args[n-1]
			}
			return ""
		case strings.HasPrefix(m, "{env:"):
			v, set := os.LookupEnv(sub[2])
			switch sub[4] {
			case ":-":
				if v == "" {
					return sub[5]
				}
			case "-":
				if !set {
					return sub[5]
				}
			}
			return v
		default:
			return strings.Join(shifted, " ")
		}
	})
}

func shiftArgs(args []string, n int) []string {
	if n <= 0 {
		return args
	}
	if n >= len(args) {
		return nil
	}
	return args[n:]
}

func hasPrefix(args, prefix []string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

// runPager reads keystrokes the way less does and reports each one it
// receives, quitting on "q". It reads fd 2 directly — less's last-resort
// input source, the same terminal ttyname(2) names — so it exercises the
// DEVX-1049 defect (keystrokes must be fed into lstk's inner PTY) without
// depending on the host's installed pager. Raw mode makes each keystroke
// arrive on its own, without a newline, as a real pager arranges.
func runPager() {
	fd := int(os.Stderr.Fd())
	prev, err := term.MakeRaw(fd)
	if err != nil {
		fail("pager: cannot enter raw mode on the stderr terminal", err)
	}
	defer func() { _ = term.Restore(fd, prev) }()
	buf := make([]byte, 1)
	for {
		n, err := os.Stderr.Read(buf)
		if err != nil || n == 0 {
			fail("pager: cannot read keystroke from the stderr terminal", err)
		}
		fmt.Printf("PAGER_GOT:%s\r\n", buf[:n])
		if buf[0] == 'q' {
			return
		}
	}
}

func fail(msg string, err error) {
	fmt.Fprintf(os.Stderr, "faketool: %s: %v\n", msg, err)
	os.Exit(111)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		fail("cannot locate own executable", err)
	}
	data, err := os.ReadFile(exe + ".fakecfg")
	if err != nil {
		fail("cannot read config", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fail("cannot parse config", err)
	}

	args := os.Args[1:]
	for _, c := range cfg.Cases {
		if !hasPrefix(args, c.Args) {
			continue
		}
		caseShifted := shiftArgs(args, c.Shift)
		for _, line := range c.Stdout {
			fmt.Println(expand(line, args, caseShifted))
		}
		for _, line := range c.Stderr {
			fmt.Fprintln(os.Stderr, expand(line, args, caseShifted))
		}
		os.Exit(c.ExitCode)
	}

	if cfg.SleepSeconds > 0 {
		time.Sleep(time.Duration(cfg.SleepSeconds) * time.Second)
	}

	shifted := shiftArgs(args, cfg.Shift)

	if cfg.RecordFile != "" {
		if err := os.WriteFile(cfg.RecordFile, []byte(expand(cfg.RecordContent, args, shifted)), 0o644); err != nil {
			fail("cannot write record file", err)
		}
	}
	for _, line := range cfg.Stdout {
		fmt.Println(expand(line, args, shifted))
	}
	for _, line := range cfg.Stderr {
		fmt.Fprintln(os.Stderr, expand(line, args, shifted))
	}
	if cfg.Pager {
		runPager()
	}
	if cfg.DumpFile != "" {
		if f, err := os.Open(cfg.DumpFile); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				fmt.Println(cfg.DumpPrefix + scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				fail("cannot read dump file", err)
			}
			_ = f.Close()
		}
	}
	os.Exit(cfg.ExitCode)
}

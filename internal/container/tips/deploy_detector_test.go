package tips

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/extension"
	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deploy tip tests run a copy of this test binary as the stub
// lstk-deploy; these variables switch it into stub mode (see TestMain).
const (
	deployStubToolsEnv  = "LSTK_TEST_DEPLOY_STUB_TOOLS"
	deployStubModeEnv   = "LSTK_TEST_DEPLOY_STUB_MODE"
	deployStubRecordEnv = "LSTK_TEST_DEPLOY_STUB_RECORD"
)

func TestMain(m *testing.M) {
	if mode, ok := os.LookupEnv(deployStubModeEnv); ok {
		os.Exit(runDeployStub(mode))
	}
	os.Exit(m.Run())
}

func runDeployStub(mode string) int {
	if record := os.Getenv(deployStubRecordEnv); record != "" {
		content := strings.Join(os.Args[1:], " ") + "\n" + os.Getenv(extension.EnvContext)
		_ = os.WriteFile(record, []byte(content), 0o644)
	}
	switch mode {
	case "exit":
		return 3
	case "garbage":
		fmt.Print("not json")
		return 0
	}
	tools := []string{}
	if v := os.Getenv(deployStubToolsEnv); v != "" {
		tools = strings.Split(v, ",")
	}
	data, _ := json.Marshal(map[string]any{"tools": tools})
	fmt.Printf(`{"schemaVersion":1,"command":"deploy detect","status":"ok","data":%s}`, data)
	return 0
}

// deployStubResolver returns a Resolver whose bundled dir holds only a stub
// lstk-deploy, with PATH emptied so no real extension can be resolved.
func deployStubResolver(t *testing.T, mode, tools string) (*extension.Resolver, string) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	bin, err := os.ReadFile(exe)
	require.NoError(t, err)

	dir := t.TempDir()
	name := "lstk-deploy"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), bin, 0o755))

	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("PATH", "")
	t.Setenv(deployStubModeEnv, mode)
	t.Setenv(deployStubToolsEnv, tools)
	t.Setenv(deployStubRecordEnv, record)

	r := extension.NewResolver(log.Nop())
	r.BundledDir = dir
	return r, record
}

func TestDeployTip_RendersDetectedTools(t *testing.T) {
	for _, tc := range []struct {
		tools string
		want  string
	}{
		{"terraform", "> Tip: Deploy your Terraform project to LocalStack: lstk deploy"},
		{"cdk", "> Tip: Deploy your CDK project to LocalStack: lstk deploy"},
		{"sam", "> Tip: Deploy your SAM project to LocalStack: lstk deploy"},
		{"terraform,cdk", "> Tip: Deploy your infrastructure as code to LocalStack: lstk deploy"},
		{"pulumi", "> Tip: Deploy your infrastructure as code to LocalStack: lstk deploy"},
	} {
		t.Run(tc.tools, func(t *testing.T) {
			resolver, _ := deployStubResolver(t, "ok", tc.tools)
			tip := NewDeployTip(resolver, "/cfg", "tok")

			require.True(t, tip.isEligible(config.EmulatorAWS))
			got, ok := tip.detect(t.Context())

			assert.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeployTip_RunsDetectQuietlyWithToken(t *testing.T) {
	resolver, record := deployStubResolver(t, "ok", "terraform")

	_, ok := NewDeployTip(resolver, "/cfg", "tok").detect(t.Context())
	require.True(t, ok)

	content, err := os.ReadFile(record)
	require.NoError(t, err)
	args, rawCtx, _ := strings.Cut(string(content), "\n")
	assert.Equal(t, "detect", args)

	var ctx map[string]any
	require.NoError(t, json.Unmarshal([]byte(rawCtx), &ctx))
	assert.Equal(t, "tok", ctx["authToken"])
	assert.Equal(t, "/cfg", ctx["configDir"])
	assert.Equal(t, true, ctx["json"])
	assert.Equal(t, true, ctx["nonInteractive"])
	assert.NotContains(t, ctx, "sessionId")
	assert.NotContains(t, ctx, "machineId")
}

func TestDeployTip_DeclinesWithoutUsableResult(t *testing.T) {
	for _, tc := range []struct{ name, mode, tools string }{
		{"no tools", "ok", ""},
		{"non-zero exit", "exit", ""},
		{"garbage output", "garbage", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, _ := deployStubResolver(t, tc.mode, tc.tools)

			_, ok := NewDeployTip(resolver, "/cfg", "tok").detect(t.Context())

			assert.False(t, ok)
		})
	}
}

func TestDeployTip_IneligibleWithoutAWSOrExtension(t *testing.T) {
	resolver, record := deployStubResolver(t, "ok", "terraform")
	tip := NewDeployTip(resolver, "/cfg", "tok")
	for _, emulatorType := range []config.EmulatorType{config.EmulatorSnowflake, config.EmulatorAzure} {
		assert.False(t, tip.isEligible(emulatorType), "%s is not an IaC deploy target", emulatorType)
	}

	missing := extension.NewResolver(log.Nop())
	missing.BundledDir = t.TempDir()
	assert.False(t, NewDeployTip(missing, "/cfg", "tok").isEligible(config.EmulatorAWS), "no deploy extension resolves")

	assert.NoFileExists(t, record, "eligibility never runs the extension")
}

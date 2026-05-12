package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/sandbox"
	"github.com/spf13/cobra"
)

func newSandboxCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage cloud-hosted LocalStack sandbox instances",
		Long:  "Manage cloud-hosted LocalStack sandbox instances.",
	}
	cmd.AddCommand(
		newSandboxCreateCmd(cfg, logger),
		newSandboxListCmd(cfg, logger),
		newSandboxDescribeCmd(cfg, logger),
		newSandboxDeleteCmd(cfg, logger),
		newSandboxLogsCmd(cfg, logger),
		newSandboxURLCmd(cfg, logger),
		newSandboxResetCmd(cfg, logger),
	)
	return cmd
}

func newSandboxCreateCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var (
		name    string
		timeout int
		envVars []string
	)
	cmd := &cobra.Command{
		Use:     "create <name>",
		Short:   "Create a sandbox instance",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0")
			}
			parsedEnv, err := parseSandboxEnv(envVars)
			if err != nil {
				return err
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			body, err := client.Create(cmd.Context(), sandbox.CreateOptions{
				Name:            name,
				LifetimeMinutes: timeout,
				EnvVars:         parsedEnv,
			})
			if err != nil {
				return err
			}
			emitRawJSON(output.NewPlainSink(os.Stdout), body)
			return nil
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", 60, "Instance lifetime in minutes")
	cmd.Flags().StringArrayVarP(&envVars, "env", "e", nil, "Environment variable to pass to the instance (KEY=VALUE)")
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxListCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List sandbox instances",
		Args:    cobra.NoArgs,
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			instances, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			sink := output.NewPlainSink(os.Stdout)
			if len(instances) == 0 {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No sandbox instances found"})
				return nil
			}
			rows := make([][]string, 0, len(instances))
			for _, inst := range instances {
				rows = append(rows, []string{inst.Name, inst.Status, inst.Endpoint, inst.Expires})
			}
			sink.Emit(output.TableEvent{
				Headers: []string{"Name", "Status", "Endpoint", "Expires"},
				Rows:    rows,
			})
			return nil
		},
	}
	return cmd
}

func newSandboxDescribeCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "describe <name>",
		Short:   "Show the current state of a sandbox instance",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			instance, err := client.Describe(cmd.Context(), name)
			if err != nil {
				if errors.Is(err, sandbox.ErrNotFound) {
					return fmt.Errorf("sandbox instance %q not found", name)
				}
				return err
			}
			emitRawJSON(output.NewPlainSink(os.Stdout), []byte(fmt.Sprintf(`{"name":%q,"status":%q,"endpoint":%q,"expires":%q}`, instance.Name, instance.Status, instance.Endpoint, instance.Expires)))
			return nil
		},
	}
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxDeleteCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var (
		name        string
		wait        bool
		waitTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Short:   "Delete a sandbox instance",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			if waitTimeout <= 0 {
				return fmt.Errorf("--wait-timeout must be greater than 0")
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			sink := output.NewPlainSink(os.Stdout)

			if err := client.Delete(cmd.Context(), name); err != nil {
				if errors.Is(err, sandbox.ErrNotFound) {
					return fmt.Errorf("sandbox instance %q not found", name)
				}
				return err
			}

			if wait {
				if err := client.WaitForDeletion(cmd.Context(), sink, name, waitTimeout); err != nil {
					return err
				}
			}

			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Deleted sandbox instance %q", name)})
			return nil
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait until the instance is fully deleted")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 5*time.Minute, "Maximum time to wait for deletion when --wait is set")
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxLogsCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "logs <name>",
		Short:   "Fetch logs from a sandbox instance",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			lines, err := client.Logs(cmd.Context(), name)
			if err != nil {
				if errors.Is(err, sandbox.ErrNotFound) {
					return fmt.Errorf("sandbox instance %q not found", name)
				}
				return err
			}
			sink := output.NewPlainSink(os.Stdout)
			if len(lines) == 0 {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No logs available for this instance"})
				return nil
			}
			for _, line := range lines {
				sink.Emit(output.LogLineEvent{Source: output.LogSourceEmulator, Line: line})
			}
			return nil
		},
	}
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxURLCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "url <name>",
		Short:   "Print the sandbox endpoint URL",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			endpoint, err := resolveSandboxEndpoint(cmd.Context(), client, name)
			if err != nil {
				return err
			}
			output.NewPlainSink(os.Stdout).Emit(output.MessageEvent{Text: endpoint})
			return nil
		},
	}
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxResetCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "reset <name>",
		Short:   "Reset all state in a running sandbox instance",
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandboxName(args, name)
			if err != nil {
				return err
			}
			client, err := newSandboxClient(cfg, logger)
			if err != nil {
				return err
			}
			endpoint, err := resolveSandboxEndpoint(cmd.Context(), client, name)
			if err != nil {
				return err
			}
			if err := client.ResetState(cmd.Context(), endpoint); err != nil {
				return err
			}
			output.NewPlainSink(os.Stdout).Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Reset sandbox instance %q", name)})
			return nil
		},
	}
	addHiddenSandboxNameFlag(cmd, &name)
	return cmd
}

func newSandboxClient(cfg *env.Env, logger log.Logger) (*sandbox.Client, error) {
	if cfg.AuthToken == "" {
		return nil, fmt.Errorf("authentication required: run `lstk login` or set LOCALSTACK_AUTH_TOKEN")
	}
	return sandbox.NewClient(cfg.APIEndpoint, cfg.AuthToken, logger), nil
}

func addHiddenSandboxNameFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "name", "", "Name of the sandbox instance")
	_ = cmd.Flags().MarkHidden("name")
}

func resolveSandboxEndpoint(ctx context.Context, client *sandbox.Client, name string) (string, error) {
	instance, err := client.Describe(ctx, name)
	if err != nil {
		if errors.Is(err, sandbox.ErrNotFound) {
			return "", fmt.Errorf("sandbox instance %q not found", name)
		}
		return "", err
	}
	if instance.Endpoint == "" {
		return "", fmt.Errorf("sandbox instance %q has no endpoint URL", name)
	}
	return instance.Endpoint, nil
}

func sandboxName(args []string, flagValue string) (string, error) {
	if len(args) == 1 && flagValue != "" {
		return "", fmt.Errorf("provide the sandbox name either as an argument or with --name, not both")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	return "", fmt.Errorf("sandbox name is required")
}

func parseSandboxEnv(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("invalid environment variable %q: expected KEY=VALUE", value)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid environment variable %q: key cannot be empty", value)
		}
		result[key] = strings.TrimSpace(val)
	}
	return result, nil
}

func emitRawJSON(sink output.Sink, body []byte) {
	sink.Emit(output.MessageEvent{Text: strings.TrimSpace(string(body))})
}

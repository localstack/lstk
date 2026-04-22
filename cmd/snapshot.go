package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newSnapshotCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator state snapshots",
		Long:  "Manage snapshots of your emulator state.\n\nRemote operations (save, load, list, delete, versions) require you to be logged in.\nLocal file operations (export, import) work without logging in.",
	}
	cmd.AddCommand(
		newSnapshotSaveCmd(cfg, tel),
		newSnapshotLoadCmd(cfg, tel),
		newSnapshotExportCmd(cfg, tel),
		newSnapshotImportCmd(cfg, tel),
		newSnapshotListCmd(cfg, tel),
		newSnapshotDeleteCmd(cfg, tel),
		newSnapshotVersionsCmd(cfg, tel),
	)
	return cmd
}

// requireSnapshotAuth emits a clear error and returns a SilentError when no token is available.
func requireSnapshotAuth(cfg *env.Env, sink output.Sink) error {
	if cfg.AuthToken != "" {
		return nil
	}
	output.EmitError(sink, output.ErrorEvent{
		Title:   "remote snapshot operations require authentication",
		Summary: "Run 'lstk login' or set LOCALSTACK_AUTH_TOKEN.",
	})
	return output.NewSilentError(fmt.Errorf("not authenticated"))
}

// resolveEmulatorHost returns the best host:port for reaching LocalStack.
func resolveEmulatorHost(cfg *env.Env) (string, error) {
	appConfig, err := config.Get()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	if len(appConfig.Containers) == 0 {
		return "", fmt.Errorf("no container configured")
	}
	port := appConfig.Containers[0].Port
	host, _ := endpoint.ResolveHost(port, cfg.LocalStackHost)
	return host, nil
}

// --- save ---

func newSnapshotSaveCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save NAME",
		Short: "Save a snapshot to remote",
		Long:  "Save the running emulator's state as a named remote snapshot on the LocalStack platform.",
		Args:  cobra.ExactArgs(1),
		PreRunE: initConfig,
		RunE: commandWithTelemetry("snapshot save", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)
			if err := requireSnapshotAuth(cfg, sink); err != nil {
				return err
			}

			host, err := resolveEmulatorHost(cfg)
			if err != nil {
				return err
			}

			services, _ := cmd.Flags().GetString("services")
			message, _ := cmd.Flags().GetString("message")
			visibility, _ := cmd.Flags().GetString("visibility")

			opts := snapshot.SaveOptions{
				PodName:    args[0],
				Token:      cfg.AuthToken,
				Message:    message,
				Visibility: visibility,
			}
			if services != "" {
				opts.Services = splitServices(services)
			}

			client := snapshot.NewEmulatorClient(host)
			_, err = snapshot.Save(cmd.Context(), client, sink, opts)
			return err
		}),
	}
	cmd.Flags().String("services", "", "Comma-separated list of services to include (default: all)")
	cmd.Flags().StringP("message", "m", "", "Description for this version")
	cmd.Flags().String("visibility", "", "Set visibility: public or private")
	return cmd
}

// --- load ---

func newSnapshotLoadCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load SOURCE",
		Short: "Load a snapshot from remote",
		Long:  "Load a remote snapshot into the running emulator. Use NAME:N to load a specific version.",
		Args:  cobra.ExactArgs(1),
		PreRunE: initConfig,
		RunE: commandWithTelemetry("snapshot load", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)
			if err := requireSnapshotAuth(cfg, sink); err != nil {
				return err
			}

			host, err := resolveEmulatorHost(cfg)
			if err != nil {
				return err
			}

			name, version := parsePodVersion(args[0])
			strategy, _ := cmd.Flags().GetString("strategy")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			opts := snapshot.LoadOptions{
				PodName:  name,
				Version:  version,
				Strategy: strategy,
				DryRun:   dryRun,
				Token:    cfg.AuthToken,
			}

			client := snapshot.NewEmulatorClient(host)
			_, err = snapshot.Load(cmd.Context(), client, sink, opts)
			return err
		}),
	}
	cmd.Flags().String("strategy", "account-region-merge", "Merge strategy: account-region-merge, overwrite, service-merge")
	cmd.Flags().Bool("dry-run", false, "Preview what would be loaded without applying")
	cmd.Flags().Bool("yes", false, "Skip version-mismatch warning")
	return cmd
}

// --- export ---

func newSnapshotExportCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [PATH]",
		Short: "Export a snapshot to a local file",
		Long:  "Export the running emulator's state to a local ZIP file. No authentication required.",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: commandWithTelemetry("snapshot export", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)

			host, err := resolveEmulatorHost(cfg)
			if err != nil {
				return err
			}

			path := defaultExportPath()
			if len(args) == 1 {
				path = args[0]
			}

			services, _ := cmd.Flags().GetString("services")
			opts := snapshot.ExportOptions{Path: path}
			if services != "" {
				opts.Services = splitServices(services)
			}

			client := snapshot.NewEmulatorClient(host)
			result, err := snapshot.Export(cmd.Context(), client, sink, opts)
			if err != nil {
				return err
			}

			output.EmitSuccess(sink, fmt.Sprintf("Snapshot exported to local file: %s (%s)", result.Path, formatSnapshotBytes(result.Bytes)))
			return nil
		}),
	}
	cmd.Flags().String("services", "", "Comma-separated list of services to include (default: all)")
	return cmd
}

// --- import ---

func newSnapshotImportCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "import PATH",
		Short:   "Import a snapshot from a local file",
		Long:    "Load a previously exported snapshot ZIP file into the running emulator. No authentication required.",
		Args:    cobra.ExactArgs(1),
		PreRunE: initConfig,
		RunE: commandWithTelemetry("snapshot import", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)

			host, err := resolveEmulatorHost(cfg)
			if err != nil {
				return err
			}

			opts := snapshot.ImportOptions{Path: args[0]}
			client := snapshot.NewEmulatorClient(host)
			result, err := snapshot.Import(cmd.Context(), client, sink, opts)
			if err != nil {
				return err
			}

			output.EmitSuccess(sink, fmt.Sprintf("Snapshot loaded from local file: %s", result.Path))
			if len(result.Services) > 0 {
				output.EmitSecondary(sink, "Services restored: "+strings.Join(result.Services, ", "))
			}
			return nil
		}),
	}
}

// --- list ---

func newSnapshotListCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List remote snapshots",
		Long:  "List your remote snapshots. Does not require LocalStack to be running.",
		RunE: commandWithTelemetry("snapshot list", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)
			if err := requireSnapshotAuth(cfg, sink); err != nil {
				return err
			}
			client := snapshot.NewPlatformClient(cfg.APIEndpoint, cfg.AuthToken)
			_, err := snapshot.List(cmd.Context(), client, sink)
			return err
		}),
	}
	cmd.Flags().String("format", "table", "Output format: table or json")
	return cmd
}

// --- delete ---

func newSnapshotDeleteCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a remote snapshot",
		Long:  "Delete a remote snapshot and all its versions. This is irreversible. Does not require LocalStack to be running.",
		Args:  cobra.ExactArgs(1),
		RunE: commandWithTelemetry("snapshot delete", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)
			if err := requireSnapshotAuth(cfg, sink); err != nil {
				return err
			}

			name := args[0]
			skipConfirm, _ := cmd.Flags().GetBool("yes")

			if !skipConfirm {
				if !isInteractiveMode(cfg) {
					return fmt.Errorf("use --yes to confirm deletion in non-interactive mode")
				}
				fmt.Fprintf(os.Stderr, "About to delete '%s' and all its versions. This cannot be undone.\nConfirm? [y/N] ", name)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					output.EmitNote(sink, "Deletion cancelled.")
					return nil
				}
			}

			client := snapshot.NewPlatformClient(cfg.APIEndpoint, cfg.AuthToken)
			return snapshot.Delete(cmd.Context(), client, sink, name)
		}),
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// --- versions ---

func newSnapshotVersionsCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions NAME",
		Short: "List versions of a remote snapshot",
		Long:  "List available versions of a remote snapshot. Does not require LocalStack to be running.",
		Args:  cobra.ExactArgs(1),
		RunE: commandWithTelemetry("snapshot versions", tel, func(cmd *cobra.Command, args []string) error {
			sink := output.NewPlainSink(os.Stdout)
			if err := requireSnapshotAuth(cfg, sink); err != nil {
				return err
			}
			client := snapshot.NewPlatformClient(cfg.APIEndpoint, cfg.AuthToken)
			_, err := snapshot.Versions(cmd.Context(), client, sink, args[0])
			return err
		}),
	}
	cmd.Flags().String("format", "table", "Output format: table or json")
	return cmd
}

// --- helpers ---

func splitServices(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parsePodVersion splits "name:3" into ("name", 3). Returns version 0 if absent.
func parsePodVersion(s string) (name string, version int) {
	idx := strings.LastIndex(s, ":")
	if idx == -1 {
		return s, 0
	}
	n := 0
	fmt.Sscanf(s[idx+1:], "%d", &n)
	if n > 0 {
		return s[:idx], n
	}
	return s, 0
}

func defaultExportPath() string {
	return fmt.Sprintf("snapshot-%s.zip", time.Now().Format("20060102-150405"))
}

func formatSnapshotBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

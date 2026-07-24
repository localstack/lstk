package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/mcpconfig"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newMCPCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage the LocalStack MCP server integration",
		Long:  "Manage the LocalStack Model Context Protocol (MCP) server integration so coding agents can drive LocalStack. Use 'lstk mcp init' to configure your installed MCP clients.",
	}
	cmd.AddCommand(newMCPInitCmd(cfg))
	return cmd
}

func newMCPInitCmd(cfg *env.Env) *cobra.Command {
	var (
		method    string
		token     string
		imageTag  string
		cacheDir  string
		workspace string
		clients   []string
		extraEnv  []string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure MCP clients to use the LocalStack MCP server",
		Long:  "Configure your installed MCP clients (Cursor, Claude Code, Claude Desktop, VS Code, Codex) to launch the LocalStack MCP server. Defaults to running the server in Docker (with access to your Docker socket so it can manage LocalStack containers), so no Node toolchain is required; use --method npx to run it via Node instead. The auth token is reused from your environment or 'lstk login'. By default every detected client is configured; pass --client to narrow the selection.",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedToken := token
			if resolvedToken == "" {
				resolvedToken = cfg.AuthToken
			}

			parsedEnv, err := parseEnvAssignments(extraEnv)
			if err != nil {
				return err
			}

			resolvedCacheDir := cacheDir
			if resolvedCacheDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("could not resolve home directory: %w", err)
				}
				resolvedCacheDir = filepath.Join(home, ".localstack-mcp")
			}

			opts := mcpconfig.Options{
				Token:     resolvedToken,
				Method:    mcpconfig.Method(method),
				ExtraEnv:  parsedEnv,
				ClientIDs: clients,
				Docker: mcpconfig.DockerOptions{
					CacheDir:     resolvedCacheDir,
					WorkspaceDir: workspace,
					ImageTag:     imageTag,
				},
			}

			if isInteractiveMode(cfg) {
				return ui.RunMCPInit(cmd.Context(), opts)
			}
			return mcpconfig.RunInit(cmd.Context(), output.NewPlainSink(os.Stdout), opts)
		},
	}

	cmd.Flags().StringVar(&method, "method", string(mcpconfig.MethodDocker), "How clients launch the server: docker or npx")
	cmd.Flags().StringSliceVar(&clients, "client", nil, "MCP clients to configure (default: all detected); repeatable or comma-separated: "+strings.Join(mcpconfig.SupportedClientIDs(), ", "))
	cmd.Flags().StringVar(&token, "token", "", "LocalStack auth token (default: from environment or 'lstk login')")
	cmd.Flags().StringVar(&imageTag, "image-tag", "latest", "Docker image tag for the MCP server (docker method)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Host directory for the server's cache (docker method; default: ~/.localstack-mcp)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Host directory to mount into the server so its IaC tools can see your project (docker method; default: none)")
	// StringArray (not StringSlice) so values containing commas — e.g.
	// SERVICES=s3,sqs,lambda — are kept verbatim instead of being split.
	cmd.Flags().StringArrayVar(&extraEnv, "config", nil, "Extra LocalStack env var forwarded to the server, as KEY=VALUE; repeat the flag for multiple")

	return cmd
}

// parseEnvAssignments parses KEY=VALUE pairs into a map, erroring on malformed input.
func parseEnvAssignments(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid --config %q: expected KEY=VALUE", pair)
		}
		out[key] = value
	}
	return out, nil
}

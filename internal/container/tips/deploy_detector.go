package tips

import (
	"context"
	"encoding/json"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/extension"
)

// NewDeployTip suggests `lstk deploy` when `lstk deploy detect` finds IaC in
// the working directory. It is eligible only for AWS and when a deploy
// extension resolves; resolving is a filesystem lookup, and detect runs only
// once the tip is picked. authToken is what extension dispatch conveys
// (cfg.AuthToken).
func NewDeployTip(resolver *extension.Resolver, configDir, authToken string) Tip {
	return Tip{
		eligible: func(emulatorType config.EmulatorType) bool {
			if emulatorType != config.EmulatorAWS {
				return false
			}
			_, err := resolver.Resolve("deploy")
			return err == nil
		},
		detect: func(ctx context.Context) (string, bool) {
			ext, err := resolver.Resolve("deploy")
			if err != nil {
				return "", false
			}
			data, err := extension.InvokeQuietlyWithJSON(ctx, ext, []string{"detect"},
				extension.QuietOptions{ConfigDir: configDir, AuthToken: authToken})
			if err != nil {
				return "", false
			}
			var detected struct {
				Tools []string `json:"tools"`
			}
			if err := json.Unmarshal(data, &detected); err != nil || len(detected.Tools) == 0 {
				return "", false
			}
			if len(detected.Tools) == 1 {
				if name, ok := deployToolName(detected.Tools[0]); ok {
					return "> Tip: Deploy your " + name + " project to LocalStack: lstk deploy", true
				}
			}
			return "> Tip: Deploy your infrastructure as code to LocalStack: lstk deploy", true
		},
	}
}

// deployToolName maps a `lstk deploy detect` tool id to its display name; an
// unknown id gets the generic text, so a tool the extension adds later still
// yields a tip.
func deployToolName(id string) (string, bool) {
	switch id {
	case "terraform":
		return "Terraform", true
	case "cdk":
		return "CDK", true
	case "sam":
		return "SAM", true
	}
	return "", false
}

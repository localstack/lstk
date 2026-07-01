package awsconfig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/ini.v1"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

const (
	ProfileName       = "localstack"
	configSectionName = "profile localstack" // ~/.aws/config uses "profile <name>" as section header
	credsSectionName  = "localstack"         // ~/.aws/credentials uses just the profile name
	// TODO: make region configurable (e.g. from container env or lstk config)
	defaultRegion = "us-east-1"
)

func credentialsDefaults() map[string]string {
	return map[string]string{
		"aws_access_key_id":     "test",
		"aws_secret_access_key": "test",
	}
}

// isValidLocalStackEndpoint returns true if endpoint_url in ~/.aws/config points to
// the same LocalStack instance as resolvedHost. localhost, 127.0.0.1, and
// localhost.localstack.cloud are treated as interchangeable since all three
// resolve to the local machine.
func isValidLocalStackEndpoint(endpointURL, resolvedHost string) bool {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == resolvedHost {
		return true
	}
	// If the resolved host is one of the two known local hostnames, accept the
	// other as equally valid — they both reach the same local service.
	resolvedHostname, resolvedPort, err := net.SplitHostPort(resolvedHost)
	if err != nil || !isLocalStackLocalHost(resolvedHostname) {
		return false
	}
	return u.Port() == resolvedPort && isLocalStackLocalHost(u.Hostname())
}

func isLocalStackLocalHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == endpoint.Hostname
}

func awsPaths() (configPath, credentialsPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".aws", "config"), filepath.Join(home, ".aws", "credentials"), nil
}

// profileStatus holds which AWS profile files need to be written or updated.
type profileStatus struct {
	configNeeded bool
	credsNeeded  bool
}

func (s profileStatus) anyNeeded() bool {
	return s.configNeeded || s.credsNeeded
}

// CheckProfileStatus determines which AWS profile files need to be written or updated.
func CheckProfileStatus(resolvedHost string) (profileStatus, error) {
	configPath, credsPath, err := awsPaths()
	if err != nil {
		return profileStatus{}, err
	}
	configNeeded, err := configNeedsWrite(configPath, resolvedHost)
	if err != nil {
		return profileStatus{}, err
	}
	credsNeeded, err := credsNeedWrite(credsPath)
	if err != nil {
		return profileStatus{}, err
	}
	return profileStatus{configNeeded: configNeeded, credsNeeded: credsNeeded}, nil
}

func configNeedsWrite(path, resolvedHost string) (bool, error) {
	f, err := ini.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	section, err := f.GetSection(configSectionName)
	if err != nil {
		return true, nil // section doesn't exist
	}
	endpointKey, err := section.GetKey("endpoint_url")
	if err != nil || !isValidLocalStackEndpoint(endpointKey.Value(), resolvedHost) {
		return true, nil
	}
	if !section.HasKey("region") {
		return true, nil
	}
	return false, nil
}

func credsNeedWrite(path string) (bool, error) {
	f, err := ini.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	section, err := f.GetSection(credsSectionName)
	if err != nil {
		return true, nil // section doesn't exist
	}
	for k, expected := range credentialsDefaults() {
		key, err := section.GetKey(k)
		if err != nil || key.Value() != expected {
			return true, nil
		}
	}
	return false, nil
}

// ProfileExists reports whether the localstack profile section is present in both
// ~/.aws/config and ~/.aws/credentials.
func ProfileExists(ctx context.Context) (bool, error) {
	_, span := otel.Tracer("github.com/localstack/lstk/internal/awsconfig").Start(ctx, "awsconfig.ProfileExists")
	defer span.End()

	configPath, credsPath, err := awsPaths()
	if err != nil {
		return false, err
	}
	configOK, err := sectionExists(configPath, configSectionName)
	if err != nil {
		return false, err
	}
	credsOK, err := sectionExists(credsPath, credsSectionName)
	if err != nil {
		return false, err
	}
	span.SetAttributes(attribute.Bool("awsconfig.profile_exists", configOK && credsOK))
	return configOK && credsOK, nil
}

// writeProfile writes the localstack profile to ~/.aws/config and ~/.aws/credentials,
// creating or updating sections as needed.
func writeProfile(host string) error {
	configPath, credsPath, err := awsPaths()
	if err != nil {
		return err
	}
	configKeys := map[string]string{
		"region":       defaultRegion,
		"output":       "json",
		"endpoint_url": "http://" + host,
	}
	if err := upsertSection(configPath, configSectionName, configKeys); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	if err := upsertSection(credsPath, credsSectionName, credentialsDefaults()); err != nil {
		return fmt.Errorf("failed to write %s: %w", credsPath, err)
	}
	return nil
}

func writeConfigProfile(configPath, host string) error {
	keys := map[string]string{
		"region":       defaultRegion,
		"output":       "json",
		"endpoint_url": "http://" + host,
	}
	return upsertSection(configPath, configSectionName, keys)
}

func writeCredsProfile(credsPath string) error {
	return upsertSection(credsPath, credsSectionName, credentialsDefaults())
}

func emitMissingProfileNote(sink output.Sink) {
	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "LocalStack AWS profile is incomplete. Run 'lstk setup aws'."})
}

// checkProfileSetup returns both the profile status (which files need writing) and presence (which files exist).
// This avoids loading the same files twice by combining needsProfileSetup and profilePresence.
func checkProfileSetup(resolvedHost string) (profileStatus, bool, bool, error) {
	configPath, credsPath, err := awsPaths()
	if err != nil {
		return profileStatus{}, false, false, err
	}

	status, err := CheckProfileStatus(resolvedHost)
	if err != nil {
		return profileStatus{}, false, false, err
	}

	configOK, err := sectionExists(configPath, configSectionName)
	if err != nil {
		return profileStatus{}, false, false, err
	}
	credsOK, err := sectionExists(credsPath, credsSectionName)
	if err != nil {
		return profileStatus{}, false, false, err
	}

	return status, configOK, credsOK, nil
}

// EnsureProfile checks for the LocalStack AWS profile and either emits a note when it is incomplete
// or triggers the interactive setup flow.
// resolvedHost must be a host:port string (e.g. "localhost.localstack.cloud:4566").
func EnsureProfile(ctx context.Context, sink output.Sink, interactive bool, resolvedHost string) error {
	status, configOK, credsOK, err := checkProfileSetup(resolvedHost)
	if err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("could not check AWS profile: %v", err)})
		return nil
	}
	if !status.anyNeeded() {
		return nil
	}
	if interactive && !configOK && !credsOK {
		return Setup(ctx, sink, resolvedHost, status, false)
	}

	emitMissingProfileNote(sink)
	return nil
}

// Setup checks for the localstack AWS profile and prompts to create or update it if needed.
// resolvedHost must be a host:port string (e.g. "localhost.localstack.cloud:4566").
// status is passed in from EnsureProfile to avoid re-checking the profile status.
//
// explicit is true for the user-invoked `lstk setup aws` / `lstk config profile`
// commands, where writing the profile is the command's whole purpose, so a write
// failure must surface a non-zero exit. It is false for the best-effort post-start
// convenience flow (EnsureProfile during `lstk start`), where a write failure must
// only warn and must not abort an already-running emulator.
func Setup(ctx context.Context, sink output.Sink, resolvedHost string, status profileStatus, explicit bool) error {
	if !status.anyNeeded() {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "LocalStack AWS profile is already configured."})
		return nil
	}

	configPath, credsPath, err := awsPaths()
	if err != nil {
		return reportSetupErr(sink, "could not determine AWS config paths", err, explicit)
	}

	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt:     "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?",
		Options:    []output.InputOption{{Key: "y", Label: "Y"}, {Key: "n", Label: "n"}},
		ResponseCh: responseCh,
	})

	select {
	case resp := <-responseCh:
		if resp.Cancelled {
			return nil
		}
		if resp.SelectedKey == "n" {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Skipped adding LocalStack AWS profile."})
			return nil
		}
		if status.configNeeded {
			if err := writeConfigProfile(configPath, resolvedHost); err != nil {
				return reportSetupErr(sink, "could not update ~/.aws/config", err, explicit)
			}
		}
		if status.credsNeeded {
			if err := writeCredsProfile(credsPath); err != nil {
				return reportSetupErr(sink, "could not update ~/.aws/credentials", err, explicit)
			}
		}
		if status.configNeeded && status.credsNeeded {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Created LocalStack profile in ~/.aws"})
		} else if status.configNeeded {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Created LocalStack profile in ~/.aws/config"})
		} else {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Updated LocalStack credentials in ~/.aws/credentials"})
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// reportSetupErr surfaces a profile-setup failure according to how setup was invoked.
// For the explicitly-invoked command it emits a structured error and returns a
// SilentError so the process exits non-zero; for the best-effort post-start flow it
// warns and returns nil so an already-running emulator's start is not aborted.
func reportSetupErr(sink output.Sink, msg string, err error, explicit bool) error {
	if explicit {
		sink.Emit(output.ErrorEvent{
			Title:   "Could not set up the LocalStack AWS profile",
			Summary: fmt.Sprintf("%s: %v", msg, err),
			Actions: []output.ErrorAction{{Label: "Check the permissions of ~/.aws, then re-run:", Value: "lstk setup aws"}},
		})
		return output.NewSilentError(err)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("%s: %v", msg, err)})
	return nil
}

package awsconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

const (
	profileName       = "localstack"
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

// isValidLocalStackEndpoint returns true if the stored endpoint_url matches what Setup would write.
func isValidLocalStackEndpoint(endpointURL, resolvedHost string) bool {
	return endpointURL == "http://"+resolvedHost || endpointURL == "https://"+resolvedHost
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

func (s profileStatus) filesToModify() []string {
	var files []string
	if s.configNeeded {
		files = append(files, "~/.aws/config")
	}
	if s.credsNeeded {
		files = append(files, "~/.aws/credentials")
	}
	return files
}

// checkProfileStatus determines which AWS profile files need to be written or updated.
func checkProfileStatus(configPath, credsPath, resolvedHost string) (profileStatus, error) {
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

// profileExists reports whether the localstack profile section is present in both
// ~/.aws/config and ~/.aws/credentials.
func profileExists() (bool, error) {
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

// Setup checks for the localstack AWS profile and prompts to create or update it if needed.
// In non-interactive mode, emits a note instead of prompting.
func Setup(ctx context.Context, sink output.Sink, interactive bool, port string, localStackHost string) error {
	configPath, credsPath, err := awsPaths()
	if err != nil {
		output.EmitWarning(sink, fmt.Sprintf("could not determine AWS config paths: %v", err))
		return nil
	}

	var dnsOK bool
	localStackHost, dnsOK = endpoint.ResolveHost(port, localStackHost)
	if !dnsOK {
		output.EmitNote(sink, `Could not resolve "localhost.localstack.cloud" — your system may have DNS rebind protection enabled. Using 127.0.0.1 as the endpoint.`)
	}

	status, err := checkProfileStatus(configPath, credsPath, localStackHost)
	if err != nil {
		output.EmitWarning(sink, fmt.Sprintf("could not check AWS profile: %v", err))
		return nil
	}
	if !status.anyNeeded() {
		return nil
	}

	if !interactive {
		output.EmitNote(sink, fmt.Sprintf("No complete LocalStack AWS profile found. Run lstk interactively to configure one, or add a [profile %s] section to ~/.aws/config manually.", profileName))
		return nil
	}

	files := strings.Join(status.filesToModify(), " and ")
	responseCh := make(chan output.InputResponse, 1)
	output.EmitUserInputRequest(sink, output.UserInputRequestEvent{
		Prompt:     fmt.Sprintf("Set up LocalStack AWS profile in %s?", files),
		Options:    []output.InputOption{{Key: "y", Label: "Y"}, {Key: "n", Label: "n"}},
		ResponseCh: responseCh,
	})

	select {
	case resp := <-responseCh:
		if resp.Cancelled || resp.SelectedKey == "n" {
			return nil
		}
		if status.configNeeded {
			if err := writeConfigProfile(configPath, localStackHost); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("could not update ~/.aws/config: %v", err))
				return nil
			}
		}
		if status.credsNeeded {
			if err := writeCredsProfile(credsPath); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("could not update ~/.aws/credentials: %v", err))
				return nil
			}
		}
		output.EmitSuccess(sink, fmt.Sprintf("LocalStack AWS profile written to %s", files))
		output.EmitNote(sink, fmt.Sprintf("Try: aws s3 mb s3://test --profile %s", profileName))
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}


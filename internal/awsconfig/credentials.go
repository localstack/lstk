package awsconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/ini.v1"
)

// Credentials holds static AWS credentials read from the environment or a profile.
// SessionToken is optional.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// credentialsFilePath returns the shared credentials file path, honoring
// AWS_SHARED_CREDENTIALS_FILE, defaulting to ~/.aws/credentials.
func credentialsFilePath() (string, error) {
	if p := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "credentials"), nil
}

// configFilePath returns the AWS config file path, honoring AWS_CONFIG_FILE,
// defaulting to ~/.aws/config.
func configFilePath() (string, error) {
	if p := os.Getenv("AWS_CONFIG_FILE"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "config"), nil
}

// readCredsFromSection extracts credentials from an ini section, returning ok=false
// when the access key or secret key is absent.
func readCredsFromSection(s *ini.Section) (Credentials, bool) {
	access := s.Key("aws_access_key_id").String()
	secret := s.Key("aws_secret_access_key").String()
	if access == "" || secret == "" {
		return Credentials{}, false
	}
	return Credentials{
		AccessKeyID:     access,
		SecretAccessKey: secret,
		SessionToken:    s.Key("aws_session_token").String(),
	}, true
}

// lookupProfile loads file at path and returns credentials from the section named
// sectionName, if both the file and a complete credential pair are present.
func lookupProfile(path, sectionName string) (Credentials, bool) {
	f, err := ini.Load(path)
	if err != nil {
		return Credentials{}, false
	}
	s, err := f.GetSection(sectionName)
	if err != nil {
		return Credentials{}, false
	}
	return readCredsFromSection(s)
}

// ReadProfileCredentials resolves AWS credentials for the named profile from the
// shared credentials file (~/.aws/credentials, section "[<profile>]"), falling back
// to the config file (~/.aws/config, section "[profile <profile>]", or "[default]"
// for the default profile). It honors AWS_SHARED_CREDENTIALS_FILE and AWS_CONFIG_FILE.
func ReadProfileCredentials(profile string) (Credentials, error) {
	if profile == "" {
		profile = "default"
	}

	credsPath, err := credentialsFilePath()
	if err != nil {
		return Credentials{}, err
	}
	if creds, ok := lookupProfile(credsPath, profile); ok {
		return creds, nil
	}

	configPath, err := configFilePath()
	if err != nil {
		return Credentials{}, err
	}
	// The config file names the default profile "[default]" and others
	// "[profile <name>]".
	configSection := "profile " + profile
	if profile == "default" {
		configSection = "default"
	}
	if creds, ok := lookupProfile(configPath, configSection); ok {
		return creds, nil
	}

	return Credentials{}, fmt.Errorf("could not find AWS credentials for profile %q in %s or %s", profile, credsPath, configPath)
}

// ErrNoCredentials is returned by CredentialsFromEnv when the required AWS
// credential environment variables are not set.
var ErrNoCredentials = errors.New("AWS credentials not found in environment")

// CredentialsFromEnv reads credentials from AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
// and the optional AWS_SESSION_TOKEN. It returns ErrNoCredentials when either of the
// two required variables is unset.
func CredentialsFromEnv() (Credentials, error) {
	access := os.Getenv("AWS_ACCESS_KEY_ID")
	secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if access == "" || secret == "" {
		return Credentials{}, ErrNoCredentials
	}
	return Credentials{
		AccessKeyID:     access,
		SecretAccessKey: secret,
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	}, nil
}

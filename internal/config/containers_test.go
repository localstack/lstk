package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvedEnv_ResolvesNamedEnvs(t *testing.T) {
	c := &ContainerConfig{
		Env: []string{"test", "debug"},
	}
	namedEnvs := map[string]map[string]string{
		"test":  {"iam_soft_mode": "1"},
		"debug": {"ls_log": "trace", "debug": "1"},
	}

	resolved, err := c.ResolvedEnv(namedEnvs)
	require.NoError(t, err)

	sort.Strings(resolved)
	assert.Equal(t, []string{"DEBUG=1", "IAM_SOFT_MODE=1", "LS_LOG=trace"}, resolved)
}

func TestResolvedEnv_KeysAreUppercased(t *testing.T) {
	// Viper lowercases all config keys internally; ResolvedEnv must restore them.
	c := &ContainerConfig{Env: []string{"test"}}
	namedEnvs := map[string]map[string]string{
		"test": {"iam_soft_mode": "1"},
	}

	resolved, err := c.ResolvedEnv(namedEnvs)
	require.NoError(t, err)
	assert.Equal(t, []string{"IAM_SOFT_MODE=1"}, resolved)
}

func TestResolvedEnv_ErrorOnMissingEnv(t *testing.T) {
	c := &ContainerConfig{Env: []string{"missing"}}
	_, err := c.ResolvedEnv(map[string]map[string]string{})
	assert.ErrorContains(t, err, `"missing"`)
}

func TestResolvedEnv_EmptyWhenNoEnvRefs(t *testing.T) {
	c := &ContainerConfig{}
	resolved, err := c.ResolvedEnv(map[string]map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, resolved)
}

func TestValidate_ZeroPaddedMonthTag_IsAccepted(t *testing.T) {
	for _, tag := range []string{"2026.04", "2026.04.1", "2026.04.0-amd64", "2026.01", "2026.09.2"} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			assert.NoError(t, c.Validate())
		})
	}
}

func TestNormalizeTag(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"2026.04", "2026.4"},
		{"2026.01", "2026.1"},
		{"2026.09.2", "2026.9.2"},
		{"2026.04.1", "2026.4.1"},
		{"2026.04.0-amd64", "2026.4.0-amd64"},
		{"2026.10", "2026.10"},
		{"latest", "latest"},
		{"", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeTag(tc.input))
		})
	}
}

func TestValidate_InvalidDockerTag_IsRejected(t *testing.T) {
	for _, tag := range []string{
		"my tag",                 // space
		"2026.4!",                // special char
		".hidden",                // starts with dot
		"-beta",                  // starts with hyphen
		"tag@sha",                // @ not allowed
		"foo:bar",                // colon not allowed
		strings.Repeat("a", 129), // too long
	} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			err := c.Validate()
			assert.ErrorContains(t, err, "unsupported")
		})
	}
}

func TestValidate_ValidTagFormats_AreAccepted(t *testing.T) {
	for _, tag := range []string{
		"", "latest", "stable",
		"2026.4", "2026.4.1", "2026.4.0", "2026.4.0-amd64", "2026.4.0-arm64",
		"2026.5.0.dev188",
		"2026.10", "2026.11.2",
		"3.8.0", "3.7.4",
	} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			assert.NoError(t, c.Validate())
		})
	}
}

func TestValidate_ValidPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566"}
	assert.NoError(t, c.Validate())
}

func TestAzureEmulatorResolvesStartMetadata(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAzure, Port: "4566"}

	image, err := c.Image()
	require.NoError(t, err)
	assert.Equal(t, "localstack/localstack-azure:latest", image)

	productName, err := c.ProductName()
	require.NoError(t, err)
	assert.Equal(t, "localstack-azure", productName)

	healthPath, err := c.HealthPath()
	require.NoError(t, err)
	assert.Equal(t, "/_localstack/health", healthPath)

	containerPort, err := c.ContainerPort()
	require.NoError(t, err)
	assert.Equal(t, "4566/tcp", containerPort)
}

func TestEmulatorTypeForImage_Azure(t *testing.T) {
	assert.Equal(t, EmulatorAzure, EmulatorTypeForImage("localstack/localstack-azure:latest"))
}

func TestImage_CustomImage(t *testing.T) {
	tests := []struct {
		name        string
		customImage string
		tag         string
		want        string
	}{
		{"untagged custom image gets configured tag", "my-registry.internal/localstack-pro", "2026.4", "my-registry.internal/localstack-pro:2026.4"},
		{"untagged custom image defaults to latest", "local-image-name", "", "local-image-name:latest"},
		{"tagged custom image is used as-is", "my-registry.internal/localstack-pro:custom", "latest", "my-registry.internal/localstack-pro:custom"},
		{"registry port is not mistaken for a tag", "my-registry:5000/localstack-pro", "2026.4", "my-registry:5000/localstack-pro:2026.4"},
		{"registry port with explicit tag", "my-registry:5000/localstack-pro:custom", "", "my-registry:5000/localstack-pro:custom"},
		{"digest-pinned image is used as-is", "localstack/localstack-pro@sha256:abc123def456", "2026.4", "localstack/localstack-pro@sha256:abc123def456"},
		{"registry port with digest is used as-is", "my-registry:5000/localstack-pro@sha256:abc123def456", "", "my-registry:5000/localstack-pro@sha256:abc123def456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tt.tag, CustomImage: tt.customImage}
			image, err := c.Image()
			require.NoError(t, err)
			assert.Equal(t, tt.want, image)
		})
	}
}

func TestImage_DefaultWhenNoCustomImage(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: "latest"}
	image, err := c.Image()
	require.NoError(t, err)
	assert.Equal(t, "localstack/localstack-pro:latest", image)
}

func TestSelfValidatesLicense(t *testing.T) {
	// Snowflake and Azure containers activate their own license against the
	// licensing server, so lstk skips its pre-flight platform license check.
	assert.True(t, EmulatorSnowflake.SelfValidatesLicense())
	assert.True(t, EmulatorAzure.SelfValidatesLicense())
	assert.False(t, EmulatorAWS.SelfValidatesLicense())
}

func TestValidate_MinMaxPorts(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "1"}
	assert.NoError(t, c.Validate())

	c.Port = "65535"
	assert.NoError(t, c.Validate())
}

func TestValidate_EmptyPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: ""}
	err := c.Validate()
	assert.ErrorContains(t, err, "port is required")
}

func TestValidate_NonNumericPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "abc"}
	err := c.Validate()
	assert.ErrorContains(t, err, "not a valid number")
}

func TestValidate_PortZero(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "0"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}

func TestValidate_PortTooHigh(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "65536"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}

func TestValidate_NegativePort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "-1"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}

func TestParseVolume_TwoParts(t *testing.T) {
	src := filepath.Join(t.TempDir(), "data")
	m, err := parseVolume(src+":/var/lib/localstack", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, VolumeMount{Source: src, Target: "/var/lib/localstack", ReadOnly: false}, m)
}

func TestParseVolume_ReadOnly(t *testing.T) {
	src := filepath.Join(t.TempDir(), "seed")
	m, err := parseVolume(src+":/seed:ro", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, VolumeMount{Source: src, Target: "/seed", ReadOnly: true}, m)
}

func TestParseVolume_ReadOnlyAmongOptions(t *testing.T) {
	src := filepath.Join(t.TempDir(), "seed")
	m, err := parseVolume(src+":/seed:z,ro", t.TempDir())
	require.NoError(t, err)
	assert.True(t, m.ReadOnly)
}

func TestParseVolume_RelativeSourceResolvedAgainstConfigDir(t *testing.T) {
	cfgDir := t.TempDir()
	m, err := parseVolume("./init.sf.sql:/etc/localstack/init/ready.d/init.sf.sql", cfgDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cfgDir, "init.sf.sql"), m.Source)
}

func TestParseVolume_TildeExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	m, err := parseVolume("~/scripts/x.sf.sql:/etc/localstack/init/ready.d/x.sf.sql", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "scripts/x.sf.sql"), m.Source)
}

func TestParseVolume_AbsoluteSourceUnchanged(t *testing.T) {
	src := filepath.Join(t.TempDir(), "x.sf.sql")
	m, err := parseVolume(src+":/etc/localstack/init/ready.d/x.sf.sql", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, src, m.Source)
}

func TestParseVolume_Errors(t *testing.T) {
	cases := map[string]string{
		"one part":        "/host/only",
		"four parts":      "/h:/c:ro:extra",
		"empty source":    ":/c",
		"empty target":    "/h:",
		"relative target": "/h:relative/target",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseVolume(spec, "/cfg")
			assert.Error(t, err)
		})
	}
}

func TestSplitVolumeSpec_NonWindows(t *testing.T) {
	cases := []struct {
		spec                 string
		source, target, opts string
	}{
		{"/host/data:/var/lib/localstack", "/host/data", "/var/lib/localstack", ""},
		{"./rel:/seed:ro", "./rel", "/seed", "ro"},
		// On non-Windows a single-letter host dir must NOT be treated as a drive.
		{"a:/data", "a", "/data", ""},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			source, target, opts, err := splitVolumeSpec(tc.spec, false)
			require.NoError(t, err)
			assert.Equal(t, tc.source, source)
			assert.Equal(t, tc.target, target)
			assert.Equal(t, tc.opts, opts)
		})
	}
}

func TestSplitVolumeSpec_WindowsDriveLetter(t *testing.T) {
	cases := []struct {
		spec                 string
		source, target, opts string
	}{
		{`C:\Users\me\persist:/var/lib/localstack`, `C:\Users\me\persist`, "/var/lib/localstack", ""},
		{`C:\data:/seed:ro`, `C:\data`, "/seed", "ro"},
		{"C:/forward:/seed", "C:/forward", "/seed", ""},
		// No drive letter: behaves like the normal split.
		{"./rel:/seed", "./rel", "/seed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			source, target, opts, err := splitVolumeSpec(tc.spec, true)
			require.NoError(t, err)
			assert.Equal(t, tc.source, source)
			assert.Equal(t, tc.target, target)
			assert.Equal(t, tc.opts, opts)
		})
	}
}

func TestParseVolume_ContainerTargetIsUnixAbsolute(t *testing.T) {
	// The target is always a path inside the Linux container, so a leading-slash path must be
	// accepted regardless of the host OS (filepath.IsAbs would reject it on Windows).
	m, err := parseVolume("/host/data:/var/lib/localstack", "/cfg")
	require.NoError(t, err)
	assert.Equal(t, "/var/lib/localstack", m.Target)
}

func TestVolumeDir_VolumesEntryTargetingPersistenceWins(t *testing.T) {
	persist := filepath.Join(t.TempDir(), "persist")
	extra := filepath.Join(t.TempDir(), "x.sf.sql")
	c := &ContainerConfig{
		Type:    EmulatorAWS,
		Volume:  "", // not set
		Volumes: []string{persist + ":/var/lib/localstack", extra + ":/etc/localstack/init/ready.d/x.sf.sql"},
	}
	dir, err := c.VolumeDir()
	require.NoError(t, err)
	assert.Equal(t, persist, dir)
}

func TestVolumeDir_LegacyVolumeUsedWhenNoPersistenceEntry(t *testing.T) {
	c := &ContainerConfig{
		Type:    EmulatorAWS,
		Volume:  "/legacy/persist",
		Volumes: []string{"/abs/x.sf.sql:/etc/localstack/init/ready.d/x.sf.sql"},
	}
	dir, err := c.VolumeDir()
	require.NoError(t, err)
	assert.Equal(t, "/legacy/persist", dir)
}

func TestVolumeDir_DefaultsToCacheDirWhenNeitherSet(t *testing.T) {
	cacheDir, err := os.UserCacheDir()
	require.NoError(t, err)
	c := &ContainerConfig{Type: EmulatorAWS}
	dir, err := c.VolumeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cacheDir, "lstk", "volume", c.Name()), dir)
}

func TestExtraVolumes_ExcludesPersistenceEntry(t *testing.T) {
	dir := t.TempDir()
	persist := filepath.Join(dir, "persist")
	a := filepath.Join(dir, "a.sf.sql")
	b := filepath.Join(dir, "b.sf.sql")
	c := &ContainerConfig{
		Type: EmulatorAWS,
		Volumes: []string{
			persist + ":/var/lib/localstack",
			a + ":/etc/localstack/init/ready.d/a.sf.sql",
			b + ":/etc/localstack/init/ready.d/b.sf.sql:ro",
		},
	}
	extras, err := c.ExtraVolumes()
	require.NoError(t, err)
	require.Len(t, extras, 2)
	assert.Equal(t, VolumeMount{Source: a, Target: "/etc/localstack/init/ready.d/a.sf.sql"}, extras[0])
	assert.Equal(t, VolumeMount{Source: b, Target: "/etc/localstack/init/ready.d/b.sf.sql", ReadOnly: true}, extras[1])
}

func TestValidate_RejectsMalformedVolume(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Volumes: []string{"/host/only"}}
	assert.ErrorContains(t, c.Validate(), "invalid volume")
}

func TestValidate_RejectsConflictingPersistenceSources(t *testing.T) {
	c := &ContainerConfig{
		Type:    EmulatorAWS,
		Port:    "4566",
		Volume:  "/persist/a",
		Volumes: []string{"/persist/b:/var/lib/localstack"},
	}
	assert.ErrorContains(t, c.Validate(), "persistence directory set both")
}

func TestValidate_RejectsReadOnlyPersistenceMount(t *testing.T) {
	c := &ContainerConfig{
		Type:    EmulatorAWS,
		Port:    "4566",
		Volumes: []string{"/persist:/var/lib/localstack:ro"},
	}
	assert.ErrorContains(t, c.Validate(), "cannot be mounted read-only")
}

func TestValidate_AllowsMatchingPersistenceSources(t *testing.T) {
	c := &ContainerConfig{
		Type:    EmulatorAWS,
		Port:    "4566",
		Volume:  "/persist/same",
		Volumes: []string{"/persist/same:/var/lib/localstack"},
	}
	assert.NoError(t, c.Validate())
}

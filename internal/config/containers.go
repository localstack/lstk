package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/validate"
)

type EmulatorType string

const (
	EmulatorAWS       EmulatorType = "aws"
	EmulatorSnowflake EmulatorType = "snowflake"
	EmulatorAzure     EmulatorType = "azure"
	// EmulatorSnowflakeNext is the rewritten Snowflake emulator, published as
	// localstack/snowflake-next while it is in preview. The name deliberately
	// says nothing about the implementation: at GA it takes over the plain
	// `snowflake` type and image, and the Python build stays reachable only
	// through pinned legacy tags, at which point this type is retired (LAV-595).
	EmulatorSnowflakeNext EmulatorType = "snowflake-next"

	DefaultPort    = "4566"
	dockerRegistry = "localstack"
)

var emulatorDisplayNames = map[EmulatorType]string{
	EmulatorAWS:           "AWS",
	EmulatorSnowflake:     "Snowflake",
	EmulatorAzure:         "Azure",
	EmulatorSnowflakeNext: "Snowflake Preview",
}

// SelectableEmulatorTypes lists the emulator types available for interactive selection,
// in the order they should be presented. Preview types are deliberately absent — see
// previewEmulatorTypes.
var SelectableEmulatorTypes = []EmulatorType{EmulatorAWS, EmulatorSnowflake, EmulatorAzure}

// previewEmulatorTypes lists types that are valid in config and accepted by --type,
// but are not offered by the interactive first-run picker: a new user's first choice
// should be a GA product, while an existing user can opt into a preview explicitly.
// They are still named in ParseEmulatorType's error, since an error that lists the
// valid values must list all of them.
var previewEmulatorTypes = []EmulatorType{EmulatorSnowflakeNext}

// KnownEmulatorTypes lists every type accepted in config or via --type: the
// selectable ones followed by the previews.
func KnownEmulatorTypes() []EmulatorType {
	return append(append([]EmulatorType{}, SelectableEmulatorTypes...), previewEmulatorTypes...)
}

// emulatorSelectionKeys assigns each selectable type a unique single-character key.
// "aws" and "azure" both start with 'a', so keys can't simply be the first character.
var emulatorSelectionKeys = map[EmulatorType]string{
	EmulatorAWS:       "a",
	EmulatorSnowflake: "s",
	EmulatorAzure:     "z",
}

func (e EmulatorType) SelectionKey() string {
	if key, ok := emulatorSelectionKeys[e]; ok {
		return key
	}
	return string(e)[0:1]
}

func (e EmulatorType) ShortName() string {
	if name, ok := emulatorDisplayNames[e]; ok {
		return name
	}
	return string(e)
}

func (e EmulatorType) DisplayName() string {
	return fmt.Sprintf("LocalStack %s Emulator", e.ShortName())
}

// SelfValidatesLicense reports whether the emulator container performs its own
// license activation on startup. For these emulators lstk skips its pre-flight
// platform license check (the LocalStack platform API has no catalog entry for
// them), and lets the container validate the token against the licensing server.
func (e EmulatorType) SelfValidatesLicense() bool {
	return e == EmulatorSnowflake || e == EmulatorAzure || e == EmulatorSnowflakeNext
}

var emulatorHealthPaths = map[EmulatorType]string{
	EmulatorAWS:           "/_localstack/health",
	EmulatorSnowflake:     "/_localstack/health",
	EmulatorAzure:         "/_localstack/health",
	EmulatorSnowflakeNext: "/_localstack/health",
}

var knownImages = []struct {
	Type        EmulatorType
	ProductName string
	Default     bool
}{
	{EmulatorAWS, "localstack-pro", true},
	{EmulatorAWS, "localstack", false},
	{EmulatorSnowflake, "snowflake", true},
	{EmulatorAzure, "localstack-azure", true},
	{EmulatorSnowflakeNext, "snowflake-next", true},
}

func EmulatorTypeForImage(image string) EmulatorType {
	repo, _, _ := strings.Cut(image, ":")
	for _, e := range knownImages {
		if dockerRegistry+"/"+e.ProductName == repo {
			return e.Type
		}
	}
	return ""
}

func KnownImageRepos() []string {
	repos := make([]string, len(knownImages))
	for i, e := range knownImages {
		repos[i] = dockerRegistry + "/" + e.ProductName
	}
	return repos
}

func KnownImageReposForType(t EmulatorType) []string {
	var repos []string
	for _, e := range knownImages {
		if e.Type == t {
			repos = append(repos, dockerRegistry+"/"+e.ProductName)
		}
	}
	return repos
}

type ContainerConfig struct {
	Type EmulatorType `mapstructure:"type"`
	Tag  string       `mapstructure:"tag"`
	Port string       `mapstructure:"port"`
	// CustomImage overrides the default Docker image for this emulator. Set it to use an
	// image from an internal registry or a locally loaded offline image instead of pulling
	// the default localstack image from Docker Hub. If it carries no tag, Tag (or "latest")
	// is appended; if it already carries a tag, Tag is dropped.
	CustomImage string `mapstructure:"image"`
	// CustomName overrides the derived container name (see Name). It is also exported into
	// the container as MAIN_CONTAINER_NAME, which the emulator uses to introspect itself over
	// the Docker socket and to name the containers it spawns (e.g. Lambda, as
	// <main-container-name>-lambda-<fn>-<id>). Set it when something outside lstk has to
	// address the emulator by a fixed name, such as a sidecar proxy on a CI agent.
	//
	// The key is container_name rather than a bare name because the block already carries an
	// image and a type name, against which an unqualified name reads ambiguously; the field is
	// CustomName because Name is already a method (the same pairing as CustomImage/Image).
	CustomName string `mapstructure:"container_name"`
	// Volume is the legacy single-host-directory knob for the persistence mount
	// (target /var/lib/localstack). It is still honored; new configs can express the
	// same mount as a Volumes entry targeting persistenceTarget instead.
	Volume string `mapstructure:"volume"`
	// Volumes is the umbrella list of "host:container[:ro]" bind specs. It covers
	// arbitrary mounts (e.g. Snowflake init hooks) and may also contain the persistence
	// mount (the entry targeting /var/lib/localstack).
	Volumes []string `mapstructure:"volumes"`
	// ExposePorts publishes additional container ports on the host, beyond the gateway
	// and service ports lstk publishes by default — e.g. port 53 so the emulator's DNS
	// server can be used as the host's resolver. Each entry is a bare port number
	// (published on the same host port) or a Docker-style "[host:]container[/proto]"
	// string. See ExposedPorts for the protocol rules.
	//
	// This replaces the v1 CLI's --host-dns flag; there is deliberately no CLI flag, since
	// every consumer of the value is the start path. TOML integers are accepted alongside
	// strings (expose_ports = [53, "5354:5353/udp"]) because viper's decoder is
	// WeaklyTypedInput.
	ExposePorts []string `mapstructure:"expose_ports"`
	// Env is a list of named environment references defined in the top-level [env.*] config sections.
	Env []string `mapstructure:"env"`
	// Snapshot is an optional snapshot REF (e.g. "pod:my-baseline" or a local path)
	// auto-loaded after the emulator starts. AWS emulator only. Never written by lstk:
	// `snapshot save` does not persist its destination here.
	Snapshot string `mapstructure:"snapshot"`
}

// persistenceTarget is the container path of the managed persistence/cache mount.
// The entry in Volumes targeting this path (or the legacy Volume field) defines the
// host directory backing it; lstk creates it and `lstk volume clear`/`volume path` act on it.
const persistenceTarget = "/var/lib/localstack"

// VolumeMount is a parsed bind specification with the host source resolved to an absolute path.
type VolumeMount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// parseVolume parses a "host:container[:opts]" spec. The host source is resolved to an
// absolute path: a leading "~/" is expanded to the user's home directory, and a relative
// path is joined with configDir (the directory of the config file that declared it). This
// is required because the Docker SDK treats a non-absolute source as a named volume rather
// than a bind mount. opts is a comma-separated list; only "ro" is honored.
//
// A Windows drive-letter source (e.g. "C:\\data") is handled: its drive ':' is not mistaken
// for a field separator. The container target is always a Unix (slash) absolute path.
func parseVolume(spec, configDir string) (VolumeMount, error) {
	source, target, opts, err := splitVolumeSpec(spec, runtime.GOOS == "windows")
	if err != nil {
		return VolumeMount{}, fmt.Errorf("invalid volume %q: %w", spec, err)
	}
	if source == "" {
		return VolumeMount{}, fmt.Errorf("invalid volume %q: host source is empty", spec)
	}
	if target == "" {
		return VolumeMount{}, fmt.Errorf("invalid volume %q: container target is empty", spec)
	}
	// The target is a path inside the (Linux) container, so it is validated with slash
	// semantics rather than the host OS's filepath rules.
	if !path.IsAbs(target) {
		return VolumeMount{}, fmt.Errorf("invalid volume %q: container target %q must be an absolute path", spec, target)
	}

	resolved, err := resolveHostPath(source, configDir)
	if err != nil {
		return VolumeMount{}, fmt.Errorf("invalid volume %q: %w", spec, err)
	}

	var readOnly bool
	for _, opt := range strings.Split(opts, ",") {
		if opt == "ro" {
			readOnly = true
		}
	}

	return VolumeMount{Source: resolved, Target: target, ReadOnly: readOnly}, nil
}

// splitVolumeSpec splits a "host:container[:opts]" spec into its three components. When
// windows is true, a leading drive letter on the host (e.g. "C:\\data") is rejoined so its
// ':' is not treated as a field separator — Docker applies the same rule only on Windows, so
// that a single-letter relative host dir (e.g. "a:/data") stays valid elsewhere.
func splitVolumeSpec(spec string, windows bool) (source, target, opts string, err error) {
	parts := strings.Split(spec, ":")
	if windows && len(parts) >= 2 && len(parts[0]) == 1 && isDriveLetter(parts[0][0]) &&
		(strings.HasPrefix(parts[1], `\`) || strings.HasPrefix(parts[1], "/")) {
		parts = append([]string{parts[0] + ":" + parts[1]}, parts[2:]...)
	}
	switch len(parts) {
	case 2:
		return parts[0], parts[1], "", nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("expected \"host:container\" or \"host:container:ro\"")
	}
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// resolveHostPath expands a leading "~/" and makes a relative path absolute against configDir.
func resolveHostPath(hostPath, configDir string) (string, error) {
	if hostPath == "~" || strings.HasPrefix(hostPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand ~: %w", err)
		}
		hostPath = filepath.Join(home, strings.TrimPrefix(hostPath, "~"))
	}
	if filepath.IsAbs(hostPath) {
		return hostPath, nil
	}
	return filepath.Join(configDir, hostPath), nil
}

// configDirForRelativePaths returns the directory used to resolve relative volume sources:
// the directory of the resolved config file. It falls back to the current working directory
// when no config file is in use (e.g. in-memory defaults).
func configDirForRelativePaths() string {
	cfgPath, err := ConfigFilePath()
	if err != nil || cfgPath == "" {
		return "."
	}
	return filepath.Dir(cfgPath)
}

// parsedVolumes parses every entry in Volumes, resolving sources against the config dir.
func (c *ContainerConfig) parsedVolumes() ([]VolumeMount, error) {
	configDir := configDirForRelativePaths()
	mounts := make([]VolumeMount, 0, len(c.Volumes))
	for _, spec := range c.Volumes {
		m, err := parseVolume(spec, configDir)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, m)
	}
	return mounts, nil
}

// ExtraVolumes returns the parsed bind mounts EXCLUDING the persistence entry
// (target /var/lib/localstack), which start.go mounts separately via VolumeDir.
func (c *ContainerConfig) ExtraVolumes() ([]VolumeMount, error) {
	mounts, err := c.parsedVolumes()
	if err != nil {
		return nil, err
	}
	extras := make([]VolumeMount, 0, len(mounts))
	for _, m := range mounts {
		if m.Target == persistenceTarget {
			continue
		}
		extras = append(extras, m)
	}
	return extras, nil
}

// VolumeDir returns the host directory to mount into the container for persistence/caching
// (the mount targeting /var/lib/localstack). Resolution precedence:
//  1. A Volumes entry targeting persistenceTarget — its resolved host source.
//  2. The legacy Volume field, if set — returned as-is.
//  3. The default os.UserCacheDir()/lstk/volume/<derived container name>.
//
// The default deliberately uses defaultName rather than Name: keying it to the derived name
// means setting or changing the container's `container_name` never silently orphans existing
// state, and the default path stays byte-identical to the one already-installed users have.
// Per-instance state is still expressible explicitly via Volume/Volumes.
func (c *ContainerConfig) VolumeDir() (string, error) {
	mounts, err := c.parsedVolumes()
	if err != nil {
		return "", err
	}
	for _, m := range mounts {
		if m.Target == persistenceTarget {
			return m.Source, nil
		}
	}
	if c.Volume != "" {
		return c.Volume, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "lstk", "volume", c.defaultName()), nil
}

// PortSpec is one parsed expose_ports publication: a host port bound to a container
// port for a single protocol.
type PortSpec struct {
	HostPort      string
	ContainerPort string
	Protocol      string // "tcp" or "udp"
}

func (p PortSpec) String() string {
	return p.HostPort + ":" + p.ContainerPort + "/" + p.Protocol
}

// ExposedPorts returns the publications requested via expose_ports, in config order
// and de-duplicated. An entry that names no protocol expands to both tcp and udp:
// the motivating case is the emulator's DNS server (port 53), which serves both, and
// publishing a protocol nothing listens on is harmless.
func (c *ContainerConfig) ExposedPorts() ([]PortSpec, error) {
	var specs []PortSpec
	seen := map[PortSpec]bool{}
	hostForContainer := map[string]string{}
	containerForHost := map[string]string{}
	for _, entry := range c.ExposePorts {
		parsed, err := parseExposePort(entry)
		if err != nil {
			return nil, err
		}
		for _, s := range parsed {
			if seen[s] {
				continue
			}
			// Docker keys published ports by container port and by host port, so either
			// kind of clash means one of the two entries would be silently discarded.
			containerKey := s.ContainerPort + "/" + s.Protocol
			if host, ok := hostForContainer[containerKey]; ok {
				return nil, fmt.Errorf("invalid expose_ports: container port %s is published on both host port %s and %s", containerKey, host, s.HostPort)
			}
			hostKey := s.HostPort + "/" + s.Protocol
			if container, ok := containerForHost[hostKey]; ok {
				return nil, fmt.Errorf("invalid expose_ports: host port %s is claimed by both container port %s and %s", hostKey, container, s.ContainerPort)
			}
			seen[s] = true
			hostForContainer[containerKey] = s.HostPort
			containerForHost[hostKey] = s.ContainerPort
			specs = append(specs, s)
		}
	}
	return specs, nil
}

// parseExposePort parses a single expose_ports entry of the form
// "[host:]container[/proto]"; a bare "53" publishes container port 53 on host port
// 53. One PortSpec is returned per protocol, so an entry without a protocol yields
// two (tcp and udp).
func parseExposePort(entry string) ([]PortSpec, error) {
	spec := strings.TrimSpace(entry)
	if spec == "" {
		return nil, errors.New("invalid expose_ports entry: entry is empty")
	}

	portPart, proto, hasProto := strings.Cut(spec, "/")
	protocols := []string{"tcp", "udp"}
	if hasProto {
		proto = strings.ToLower(strings.TrimSpace(proto))
		if proto != "tcp" && proto != "udp" {
			return nil, fmt.Errorf("invalid expose_ports entry %q: protocol must be \"tcp\" or \"udp\"", entry)
		}
		protocols = []string{proto}
	}

	hostPort, containerPort, hasHost := strings.Cut(portPart, ":")
	if !hasHost {
		containerPort = hostPort
	}
	if strings.Contains(containerPort, ":") {
		return nil, fmt.Errorf("invalid expose_ports entry %q: expected \"port\", \"host:container\" or \"host:container/proto\"", entry)
	}
	hostPort, err := parsePortNumber(entry, hostPort)
	if err != nil {
		return nil, err
	}
	containerPort, err = parsePortNumber(entry, containerPort)
	if err != nil {
		return nil, err
	}

	specs := make([]PortSpec, 0, len(protocols))
	for _, p := range protocols {
		specs = append(specs, PortSpec{HostPort: hostPort, ContainerPort: containerPort, Protocol: p})
	}
	return specs, nil
}

// parsePortNumber validates a port from an expose_ports entry and returns it in
// canonical form (so "0053" and "53" produce the same mapping key).
func parsePortNumber(entry, value string) (string, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid expose_ports entry %q: %q is not a valid port number", entry, value)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid expose_ports entry %q: port %d is out of range (must be 1–65535)", entry, port)
	}
	return strconv.Itoa(port), nil
}

// TagSuggestion returns an actionable hint naming a recent calendar tag and
// "latest", for messages about tags the license server cannot parse.
func TagSuggestion() string {
	y, m, _ := time.Now().Date()
	m--
	if m == 0 {
		m, y = 12, y-1
	}
	return fmt.Sprintf("try a tag like %q or \"latest\" in your config file", fmt.Sprintf("%d.%d", y, int(m)))
}

func unsupportedTagMessage() string {
	return "unsupported image tag — " + TagSuggestion()
}

// zeroPaddedMonthTagRe matches calendar-versioned tags where the month is zero-padded
// (e.g. "2026.04", "2026.04.1-amd64"). The license API does not accept zero-padded months,
// so these tags are normalized before license validation rather than rejected.
var zeroPaddedMonthTagRe = regexp.MustCompile(`^(\d{4}\.)0([1-9].*)$`)

// validTagRe mirrors Docker's tag format rules: alphanumerics, dots, hyphens, underscores;
// must not start with a dot or hyphen; max 128 characters.
var validTagRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// NormalizeTag strips a leading zero from the month in calendar-versioned tags so they
// are accepted by the license API (e.g. "2026.04" → "2026.4"). Other tags pass through unchanged.
func NormalizeTag(tag string) string {
	return zeroPaddedMonthTagRe.ReplaceAllString(tag, "${1}${2}")
}

func validateTag(tag string) error {
	if tag == "" {
		return nil
	}
	if len(tag) > 128 || !validTagRe.MatchString(tag) {
		return errors.New(unsupportedTagMessage())
	}
	return nil
}

func (c *ContainerConfig) Validate() error {
	if err := validateTag(c.Tag); err != nil {
		return err
	}
	if c.CustomName != "" {
		if err := validate.ContainerName(c.CustomName); err != nil {
			return fmt.Errorf("invalid container name %q: %w", c.CustomName, err)
		}
	}
	if c.Port == "" {
		return fmt.Errorf("port is required for %s emulator", c.Type)
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil {
		return fmt.Errorf("port %q is not a valid number", c.Port)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is out of range (must be 1–65535)", port)
	}
	if _, err := c.ExposedPorts(); err != nil {
		return err
	}
	return c.validateVolumes()
}

// validateVolumes checks each Volumes entry is structurally parseable and guards against
// declaring the persistence directory twice with conflicting sources. It does not touch the
// filesystem (existence of sources is checked at start time).
func (c *ContainerConfig) validateVolumes() error {
	mounts, err := c.parsedVolumes()
	if err != nil {
		return err
	}
	var persistenceSource string
	for _, m := range mounts {
		if m.Target == persistenceTarget {
			if m.ReadOnly {
				return fmt.Errorf("persistence directory (%s) cannot be mounted read-only", persistenceTarget)
			}
			persistenceSource = m.Source
		}
	}
	if c.Volume != "" && persistenceSource != "" {
		resolved, err := resolveHostPath(c.Volume, configDirForRelativePaths())
		if err != nil {
			return err
		}
		if resolved != persistenceSource {
			return fmt.Errorf("persistence directory set both via 'volume' and a 'volumes' entry targeting %s; use one or the other", persistenceTarget)
		}
	}
	return nil
}

// ResolvedEnv resolves the container's named environment references into KEY=value pairs.
// namedEnvs is the top-level [env.*] map from Config.
func (c *ContainerConfig) ResolvedEnv(namedEnvs map[string]map[string]string) ([]string, error) {
	var result []string
	for _, name := range c.Env {
		vars, ok := namedEnvs[name]
		if !ok {
			return nil, fmt.Errorf("environment %q referenced in container config not found", name)
		}
		for k, v := range vars {
			result = append(result, strings.ToUpper(k)+"="+v)
		}
	}
	return result, nil
}

func (c *ContainerConfig) Image() (string, error) {
	tag := c.Tag
	if tag == "" {
		tag = "latest"
	}
	if c.CustomImage != "" {
		if imageHasTag(c.CustomImage) {
			return c.CustomImage, nil
		}
		return c.CustomImage + ":" + tag, nil
	}
	productName, err := c.ProductName()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s:%s", dockerRegistry, productName, tag), nil
}

// imageHasTag reports whether a Docker image reference already includes a tag.
// A colon only counts as a tag separator when it appears in the final path
// segment, so "my-registry:5000/localstack-pro" (registry port, no tag) is
// correctly treated as untagged.
func imageHasTag(image string) bool {
	lastSegment := image[strings.LastIndex(image, "/")+1:]
	return strings.Contains(lastSegment, ":")
}

// Name returns the container name: CustomName when set, otherwise the derived defaultName.
func (c *ContainerConfig) Name() string {
	if c.CustomName != "" {
		return c.CustomName
	}
	return c.defaultName()
}

// defaultName is the derived container name: "localstack-{type}" or "localstack-{type}-{tag}" if tag != latest
func (c *ContainerConfig) defaultName() string {
	tag := c.Tag
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("localstack-%s", c.Type)
	}
	return fmt.Sprintf("localstack-%s-%s", c.Type, tag)
}

func (c *ContainerConfig) HealthPath() (string, error) {
	path, ok := emulatorHealthPaths[c.Type]
	if !ok {
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
	return path, nil
}

func (c *ContainerConfig) ContainerPort() (string, error) {
	switch c.Type {
	case EmulatorAWS, EmulatorSnowflake, EmulatorAzure, EmulatorSnowflakeNext:
		return DefaultPort + "/tcp", nil
	default:
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
}

func (c *ContainerConfig) DisplayName() string {
	return c.Type.DisplayName()
}

func (c *ContainerConfig) ProductName() (string, error) {
	for _, e := range knownImages {
		if e.Default && e.Type == c.Type {
			return e.ProductName, nil
		}
	}
	return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
}

package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/caller"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStart_RejectsMultipleContainersBeforeHealthCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	// No IsHealthy expectation: the guard must fire before the runtime is touched.
	mockRT := runtime.NewMockRuntime(ctrl)

	sink := output.NewPlainSink(io.Discard)
	opts := StartOptions{
		Logger: log.Nop(),
		Containers: []config.ContainerConfig{
			{Type: config.EmulatorAWS, Port: "4566"},
			{Type: config.EmulatorSnowflake, Port: "4567"},
		},
	}

	_, err := Start(context.Background(), mockRT, sink, opts, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one is supported at a time")
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
}

func TestCheckSingleContainer(t *testing.T) {
	assert.NoError(t, checkSingleContainer(nil))
	assert.NoError(t, checkSingleContainer([]config.ContainerConfig{{Type: config.EmulatorAWS}}))

	err := checkSingleContainer([]config.ContainerConfig{
		{Type: config.EmulatorAWS},
		{Type: config.EmulatorSnowflake},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one is supported at a time")
}

func TestStart_ReturnsEarlyIfRuntimeUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(errors.New("cannot connect to Docker daemon"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	sink := output.NewPlainSink(io.Discard)

	_, err := Start(context.Background(), mockRT, sink, StartOptions{Logger: log.Nop()}, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime not healthy")
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
}

func TestEmitPostStartPointers_WithWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorAWS, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/", false)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: localhost.localstack.cloud:4566\n")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.Contains(t, got, "> Tip:")
	assert.NotContains(t, got, "• Snowflake endpoint:",
		"AWS path must not show the snowflake-prefixed endpoint")
	assert.NotContains(t, got, "• Persistence:",
		"persistence bullet must be omitted when persist is false")
}

func TestEmitPostStartPointers_WithoutWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorAWS, "127.0.0.1:4566", "", false)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: 127.0.0.1:4566\n")
	assert.Contains(t, got, "> Tip:")
}

func TestEmitPostStartPointers_WithPersist(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorAWS, "127.0.0.1:4566", "https://app.localstack.cloud/", true)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: 127.0.0.1:4566\n• Persistence: Enabled\n• Web app: https://app.localstack.cloud\n",
		"persistence bullet must sit between the endpoint and web app lines")
}

func TestRunPostStartSetups_EmitsPersistenceFromContainerEnv(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	cfg := config.ContainerConfig{Type: config.EmulatorAWS, Tag: "latest", Port: "4566"}
	mockRT.EXPECT().ContainerEnv(gomock.Any(), cfg.Name()).Return([]string{"LOCALSTACK_PERSISTENCE=1"}, nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := runPostStartSetups(context.Background(), mockRT, sink, []config.ContainerConfig{cfg}, false, "", "", nil)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "• Persistence: Enabled",
		"persistence bullet must be emitted whenever the container env carries LOCALSTACK_PERSISTENCE=1, regardless of how it got there")
}

func TestRunPostStartSetups_OmitsPersistenceWhenContainerEnvLacksFlag(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	cfg := config.ContainerConfig{Type: config.EmulatorAWS, Tag: "latest", Port: "4566"}
	mockRT.EXPECT().ContainerEnv(gomock.Any(), cfg.Name()).Return([]string{"OTHER=1"}, nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := runPostStartSetups(context.Background(), mockRT, sink, []config.ContainerConfig{cfg}, false, "", "", nil)
	require.NoError(t, err)

	assert.NotContains(t, out.String(), "• Persistence:")
}

func TestEmitAlreadyRunning_IncludesRunningVersion(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"2026.5.3:04ddfd3a0","edition":"pro"}`))
			return
		}
		http.NotFound(w, r)
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitAlreadyRunning(context.Background(), sink, runtime.ContainerConfig{EmulatorType: config.EmulatorAWS, Port: port}, "", "", false)

	got := out.String()
	assert.Contains(t, got, "2026.5.3 is already running")
	assert.NotContains(t, got, "04ddfd3a0", "build suffix should be stripped from the version")
}

func TestEmitAlreadyRunning_FallsBackWhenVersionUnavailable(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	// Nothing is listening on this port, so the version lookup fails and we
	// fall back to the bare note.
	emitAlreadyRunning(context.Background(), sink, runtime.ContainerConfig{EmulatorType: config.EmulatorAWS, Port: "0"}, "", "", false)

	got := out.String()
	assert.Contains(t, got, "is already running")
	assert.Contains(t, got, config.EmulatorAWS.DisplayName())
}

func TestEmitPostStartPointers_Snowflake_ReplacesEndpointWithSnowflakeEndpoint(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorSnowflake, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/", false)

	got := out.String()
	assert.Contains(t, got, "• Snowflake endpoint: http://snowflake.localhost.localstack.cloud:4566\n")
	assert.NotContains(t, got, "• Endpoint: localhost.localstack.cloud:4566",
		"Snowflake should not show the bare endpoint — clients connect via the snowflake-prefixed host")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.Contains(t, got, "> Tip:")
}

func TestEmitPostStartPointers_Snowflake_OmitsPersistenceBullet(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorSnowflake, "localhost.localstack.cloud:4566", "", true)

	got := out.String()
	assert.NotContains(t, got, "• Persistence:",
		"snowflake does not support persistence; the bullet must be suppressed even when --persist is set")
}

func TestEmitPostStartPointers_Snowflake_FallsBackToBareEndpointForIPHost(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorSnowflake, "127.0.0.1:4566", "", false)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: 127.0.0.1:4566\n",
		"falls back to bare endpoint when snowflake.<host> would be invalid")
	assert.NotContains(t, got, "• Snowflake endpoint:")
	assert.Contains(t, got, "> Tip:")
}

func TestSelectContainersToStart_AttachesWhenExternalContainerOnConfiguredPort(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:3.5.0",
		Name:          "localstack-aws-3.5.0",
		EmulatorType:  config.EmulatorAWS,
		Tag:           "3.5.0",
		Port:          "4566",
		ContainerPort: "4566/tcp",
	}

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack", "localstack/snowflake", "localstack/localstack-azure"}, "4566/tcp").
		Return(&runtime.RunningContainer{Name: "external-container", Image: "localstack/localstack-pro:3.5.0", BoundPort: "4566"}, nil)
	mockRT.EXPECT().ContainerEnv(gomock.Any(), "external-container").Return(nil, nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	result, err := selectContainersToStart(context.Background(), mockRT, sink, nil, []runtime.ContainerConfig{c}, "", "")

	require.NoError(t, err)
	assert.Empty(t, result, "container should be skipped (already running)")
	assert.Contains(t, out.String(), "already running")
	assert.NotContains(t, out.String(), "config specifies")
}

func TestSelectContainersToStart_AttachesWhenExternalContainerVersionDiffers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:3.4.0",
		Name:          "localstack-aws-3.4.0",
		EmulatorType:  config.EmulatorAWS,
		Tag:           "3.4.0",
		Port:          "4566",
		ContainerPort: "4566/tcp",
	}

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack", "localstack/snowflake", "localstack/localstack-azure"}, "4566/tcp").
		Return(&runtime.RunningContainer{Name: "external-container", Image: "localstack/localstack-pro:3.5.0", BoundPort: "4566"}, nil)
	mockRT.EXPECT().ContainerEnv(gomock.Any(), "external-container").Return(nil, nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	result, err := selectContainersToStart(context.Background(), mockRT, sink, nil, []runtime.ContainerConfig{c}, "", "")

	require.NoError(t, err)
	assert.Empty(t, result, "container should be skipped (already running)")
	assert.Contains(t, out.String(), "already running")
}

func TestSelectContainersToStart_QueuesContainerWhenNoneRunningOnPort(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	// Use a free port by binding one and immediately releasing it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	freePort := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:3.5.0",
		Name:          "localstack-aws-3.5.0",
		EmulatorType:  config.EmulatorAWS,
		Tag:           "3.5.0",
		Port:          strconv.Itoa(freePort),
		ContainerPort: "4566/tcp",
	}

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack", "localstack/snowflake", "localstack/localstack-azure"}, "4566/tcp").
		Return(nil, nil)

	sink := output.NewPlainSink(io.Discard)

	result, err := selectContainersToStart(context.Background(), mockRT, sink, nil, []runtime.ContainerConfig{c}, "", "")

	require.NoError(t, err)
	assert.Equal(t, []runtime.ContainerConfig{c}, result, "container should be queued for start")
}

func TestSelectContainersToStart_ErrorsOnEmulatorTypeMismatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:         "localstack/snowflake:latest",
		Name:          "localstack-snowflake",
		EmulatorType:  config.EmulatorSnowflake,
		Tag:           "latest",
		Port:          "4566",
		ContainerPort: "4566/tcp",
	}

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack", "localstack/snowflake", "localstack/localstack-azure"}, "4566/tcp").
		Return(&runtime.RunningContainer{Name: "localstack-aws", Image: "localstack/localstack-pro:latest", BoundPort: "4566"}, nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	result, err := selectContainersToStart(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, "", "")

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
	assert.Empty(t, result)
	got := out.String()
	assert.Contains(t, got, "LocalStack AWS Emulator is running on port 4566")
	assert.Contains(t, got, "Your config specifies the LocalStack Snowflake Emulator")
	assert.Contains(t, got, "docker stop localstack-aws")
}

func TestEmitPostStartPointers_Azure(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorAzure, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/", false)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: localhost.localstack.cloud:4566\n")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.Contains(t, got, "> Tip:")
	assert.NotContains(t, got, "• Snowflake endpoint:",
		"Azure must not show the snowflake-prefixed endpoint")
}

func TestEmitPostStartPointers_UnknownEmulator_NoTip(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, config.EmulatorType("other"), "localhost.localstack.cloud:4566", "https://app.localstack.cloud/", false)

	got := out.String()
	assert.Contains(t, got, "• Endpoint: localhost.localstack.cloud:4566\n")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.NotContains(t, got, "> Tip:")
}

func TestServicePortRange_ReturnsExpectedPorts(t *testing.T) {
	ports := servicePortRange()

	// 443 is published via GATEWAY_LISTEN, not the service range.
	require.Len(t, ports, 50)
	assert.Equal(t, "4510", ports[0].ContainerPort)
	assert.Equal(t, "4510", ports[0].HostPort)
	assert.Equal(t, "4559", ports[49].ContainerPort)
	assert.Equal(t, "4559", ports[49].HostPort)
}

func TestFilterHostEnv(t *testing.T) {
	input := []string{
		"CI=true",
		"LOCALSTACK_DISABLE_EVENTS=1",
		"LOCALSTACK_API_ENDPOINT=https://example.test",
		"LOCALSTACK_AUTH_TOKEN=host-token",
		"LOCALSTACK_PERSISTENCE=1",
		"LOCALSTACK_PATH=/home/user/repos/localstack",
		"LOCALSTACK_HOME=/root",
		"LOCALSTACK_PYTHONPATH=/opt/code",
		"LOCALSTACK_LD_PRELOAD=/lib/evil.so",
		"LOCALSTACK_IFS=:",
		"LOCALSTACK_BASH_ENV=/etc/os-release",
		"LOCALSTACK_SHELL=/bin/zsh",
		"LOCALSTACK_ENV=dev",
		"LOCALSTACK_MULTILINE=a\nLOCALSTACK_PATH=",
		"LOCALSTACK_PATHFINDER=1",
		"LOCALSTACK_HOSTNAME=custom.host",
		"PATH=/usr/bin",
		"HOME=/home/user",
		"CI_PIPELINE=foo",
	}

	got, dropped := filterHostEnv(input)

	assert.Contains(t, got, "CI=true")
	assert.Contains(t, got, "LOCALSTACK_DISABLE_EVENTS=1")
	assert.Contains(t, got, "LOCALSTACK_API_ENDPOINT=https://example.test")
	assert.Contains(t, got, "LOCALSTACK_PERSISTENCE=1")
	assert.NotContains(t, got, "LOCALSTACK_AUTH_TOKEN=host-token",
		"host LOCALSTACK_AUTH_TOKEN must be filtered so it cannot overwrite the lstk-resolved token")
	assert.NotContains(t, got, "PATH=/usr/bin")
	assert.NotContains(t, got, "HOME=/home/user")
	assert.NotContains(t, got, "CI_PIPELINE=foo", "only exact CI= must be forwarded, not CI_*")
	assert.NotContains(t, got, "LOCALSTACK_PATH=/home/user/repos/localstack",
		"the emulator entrypoint strips the LOCALSTACK_ prefix, so forwarding this would override PATH inside the emulator and break startup (DEVX-984)")
	assert.NotContains(t, got, "LOCALSTACK_HOME=/root")
	assert.NotContains(t, got, "LOCALSTACK_PYTHONPATH=/opt/code")
	assert.NotContains(t, got, "LOCALSTACK_LD_PRELOAD=/lib/evil.so")
	assert.NotContains(t, got, "LOCALSTACK_IFS=:",
		"the entrypoint sources the stripped exports mid-script, so IFS would corrupt its word splitting")
	assert.NotContains(t, got, "LOCALSTACK_BASH_ENV=/etc/os-release",
		"every non-interactive bash in the container executes the file BASH_ENV names, e.g. init hooks")
	assert.Contains(t, got, "LOCALSTACK_SHELL=/bin/zsh",
		"SHELL is deliberately forwarded: nothing in the image reads it")
	assert.Contains(t, got, "LOCALSTACK_ENV=dev",
		"ENV is deliberately forwarded: only interactive shells read it, and they never inherit the re-exported env")
	assert.NotContains(t, got, "LOCALSTACK_MULTILINE=a\nLOCALSTACK_PATH=",
		"a multi-line value is split by the entrypoint's line-oriented env pipeline and can inject rogue exports like a blank PATH")
	assert.Contains(t, got, "LOCALSTACK_PATHFINDER=1", "only exact critical names are blocked after prefix stripping, not name prefixes")
	assert.Contains(t, got, "LOCALSTACK_HOSTNAME=custom.host",
		"the entrypoint excludes LOCALSTACK_HOSTNAME from prefix stripping, so it stays forwardable")
	assert.Equal(t, []droppedHostEnv{
		{name: "LOCALSTACK_PATH", overrides: "PATH"},
		{name: "LOCALSTACK_HOME", overrides: "HOME"},
		{name: "LOCALSTACK_PYTHONPATH", overrides: "PYTHONPATH"},
		{name: "LOCALSTACK_LD_PRELOAD", overrides: "LD_PRELOAD"},
		{name: "LOCALSTACK_IFS", overrides: "IFS"},
		{name: "LOCALSTACK_BASH_ENV", overrides: "BASH_ENV"},
		{name: "LOCALSTACK_MULTILINE"},
	}, dropped,
		"dropped variables are reported with the reason so start can warn the user; the intentional LOCALSTACK_AUTH_TOKEN drop is not warned about")
}

func TestAgentEnv(t *testing.T) {
	ver := version.Version()
	clientVars := []string{"LOCALSTACK_CLIENT_NAME=lstk", "LOCALSTACK_CLIENT_VERSION=" + ver}

	assert.Equal(t, append([]string{"AI_AGENT=claude-code"}, clientVars...),
		agentEnv(caller.Classification{AgentIdentity: "claude-code"}),
		"a detected agent is forwarded as AI_AGENT")
	assert.Equal(t, clientVars,
		agentEnv(caller.Classification{Interactive: true}),
		"a human start forwards no AI_AGENT but still forwards client vars")
	assert.Equal(t, clientVars,
		agentEnv(caller.Classification{CIIdentity: "github-actions"}),
		"CI without an agent still forwards client vars")
	assert.Equal(t, append([]string{"AI_AGENT=claude-code"}, clientVars...),
		agentEnv(caller.Classification{AgentIdentity: "claude-code", CIIdentity: "github-actions"}),
		"an agent running inside CI still forwards its AI_AGENT identity")
}

func TestValidateLicense_ContinuesWhenServerUnreachable(t *testing.T) {
	// A closed port yields a transport-level failure (not an *api.LicenseError),
	// which models an offline/proxy environment that cannot reach the license server.
	opts := StartOptions{
		PlatformClient: api.NewPlatformClient("http://127.0.0.1:1", log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
		Image:        "localstack/localstack-pro:2026.4",
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	_, err := validateLicense(context.Background(), sink, opts, c, "tok", filepath.Join(t.TempDir(), "license.json"))

	require.NoError(t, err, "an unreachable license server must not block the start")
	assert.Contains(t, out.String(), "Could not reach the license server")
}

func TestValidateLicense_FailsOnServerRejection(t *testing.T) {
	// A definitive rejection (HTTP 403 -> *api.LicenseError) must remain fatal.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
		Image:        "localstack/localstack-pro:2026.4",
	}

	_, err := validateLicense(context.Background(), output.NewPlainSink(io.Discard), opts, c, "tok", filepath.Join(t.TempDir(), "license.json"))

	require.Error(t, err, "a server rejection must remain fatal")
	assert.Contains(t, err.Error(), "license validation failed")
}

func TestValidateLicense_SkipsPreflightOnUnsupportedTag(t *testing.T) {
	// The server rejecting the tag *format* (e.g. a "dev" nightly or a custom
	// enterprise tag) is not a verdict on the license itself — the pre-flight must
	// be skipped so the container validates the license at startup,
	// mirroring the unreachable-server fallback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": true, "message": "licensing.license.format:illegal version string dev"}`))
	}))
	defer srv.Close()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "dev",
		Image:        "localstack/localstack-pro:dev",
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	_, err := validateLicense(context.Background(), sink, opts, c, "tok", filepath.Join(t.TempDir(), "license.json"))

	require.NoError(t, err, "a tag the license server cannot parse must not block the start")
	assert.Contains(t, out.String(), `does not support tag "dev"`)
	assert.Contains(t, out.String(), "try a tag like",
		"the warning should keep the tag suggestion so a typo'd tag stays diagnosable")
}

func TestValidateLicense_PropagatesContextCancellation(t *testing.T) {
	// A cancelled caller context (Ctrl+C) must surface as cancellation, not be
	// mistaken for an offline license server.
	opts := StartOptions{
		PlatformClient: api.NewPlatformClient("http://127.0.0.1:1", log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
		Image:        "localstack/localstack-pro:2026.4",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	_, err := validateLicense(ctx, sink, opts, c, "tok", filepath.Join(t.TempDir(), "license.json"))

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, out.String(), "Could not reach the license server",
		"a cancellation must not be reported as an unreachable license server")
}

func TestTryPrePullLicenseValidation_SkipsCheckWhenImageIsLocal(t *testing.T) {
	// A pinned image already present locally is not pulled (see pullImages), so the
	// CLI must not run a license pre-flight either — the container validates the
	// license at startup. This keeps the local-image start fully offline.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:        "my-registry.internal/localstack-pro:2026.4",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
	}
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)

	var licenseHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&licenseHits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}

	postPull, _, err := tryPrePullLicenseValidation(context.Background(), mockRT, output.NewPlainSink(io.Discard), opts, []runtime.ContainerConfig{c}, "tok", filepath.Join(t.TempDir(), "license.json"), false)

	require.NoError(t, err)
	assert.Empty(t, postPull, "a pinned local image needs no post-pull validation")
	assert.Equal(t, int32(0), atomic.LoadInt32(&licenseHits), "the license server must not be contacted for a local image")
}

func TestTryPrePullLicenseValidation_ChecksWhenImageMissing(t *testing.T) {
	// The pre-flight check still runs (and stays fatal on rejection) when the pinned
	// image is not present locally — a pull will happen, so failing fast is correct.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:2026.4",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
	}
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(false, nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}

	_, _, err := tryPrePullLicenseValidation(context.Background(), mockRT, output.NewPlainSink(io.Discard), opts, []runtime.ContainerConfig{c}, "tok", filepath.Join(t.TempDir(), "license.json"), false)

	require.Error(t, err, "a missing local image must still fail fast on a server rejection")
}

func TestStartContainers_SnowflakeLicenseError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/snowflake:latest",
		Name:          "localstack-snowflake",
		EmulatorType:  config.EmulatorSnowflake,
		Tag:           "latest",
		Port:          "4566",
		ContainerPort: "4566/tcp",
		HealthPath:    "/_localstack/health",
	}
	const containerID = "abc123"
	licenseLog := "⚠️ The Snowflake emulator is currently not covered by your license. ❄️"
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, exitResultChan(runtime.ExitResult{ExitCode: 1}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), containerID, gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return(licenseLog, nil)

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{}, 0, false, false)
	tel.Close()

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error should be silent since ErrorEvent was already emitted")
	got := out.String()
	assert.Contains(t, got, "Your license does not include the Snowflake emulator.")
	assert.Contains(t, got, "https://app.localstack.cloud/sign-up")
	assert.Contains(t, got, "https://www.localstack.cloud/demo")

	select {
	case ev := <-capturedEvents:
		payload, ok := ev["payload"].(map[string]any)
		require.True(t, ok, "telemetry event should have a payload map")
		assert.Equal(t, telemetry.LifecycleStartError, payload["event_type"])
		assert.Equal(t, telemetry.ErrCodeLicenseInvalid, payload["error_code"])
		assert.Equal(t, "snowflake", payload["emulator"])
	default:
		t.Fatal("no telemetry event received")
	}
}

func TestStartContainers_AzureLicenseError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-azure:latest",
		Name:          "localstack-azure",
		EmulatorType:  config.EmulatorAzure,
		Tag:           "latest",
		Port:          "4566",
		ContainerPort: "4566/tcp",
		HealthPath:    "/_localstack/health",
	}
	const containerID = "abc123"
	licenseLog := "The Azure emulator is currently not covered by your license."
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, exitResultChan(runtime.ExitResult{ExitCode: 1}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), containerID, gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return(licenseLog, nil)

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{}, 0, false, false)
	tel.Close()

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error should be silent since ErrorEvent was already emitted")
	got := out.String()
	assert.Contains(t, got, "Your license does not include the Azure emulator.")
	assert.Contains(t, got, "https://app.localstack.cloud/sign-up")
	assert.Contains(t, got, "https://www.localstack.cloud/demo")

	select {
	case ev := <-capturedEvents:
		payload, ok := ev["payload"].(map[string]any)
		require.True(t, ok, "telemetry event should have a payload map")
		assert.Equal(t, telemetry.LifecycleStartError, payload["event_type"])
		assert.Equal(t, telemetry.ErrCodeLicenseInvalid, payload["error_code"])
		assert.Equal(t, "azure", payload["emulator"])
	default:
		t.Fatal("no telemetry event received")
	}
}

// exitResultChan returns a buffered channel already holding res, matching the
// contract of the exit channel returned by runtime.Runtime.Start.
func exitResultChan(res runtime.ExitResult) <-chan runtime.ExitResult {
	ch := make(chan runtime.ExitResult, 1)
	ch <- res
	return ch
}

// unreachableHealthURL returns a URL pointing at a closed local port, so the
// health GET in startupMonitor.await always fails to connect.
func unreachableHealthURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return "http://" + addr + "/_localstack/health"
}

func TestStartupMonitorAwait_TimesOutWhenNeverHealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// Container stays running the whole time but never serves health.
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid").Return(true, nil).AnyTimes()

	sink := output.NewPlainSink(io.Discard)
	// exitCh never fires; a tiny non-interactive timeout should surface promptly.
	exitCh := make(chan runtime.ExitResult)

	monitor := newStartupMonitor(mockRT, sink, nil, 50*time.Millisecond, false)
	err := monitor.await(context.Background(), "cid", unreachableHealthURL(t), exitCh)

	var timeoutErr *startupTimeoutError
	require.ErrorAs(t, err, &timeoutErr)
	assert.False(t, timeoutErr.stopped, "non-interactive timeout leaves the container running")
}

// The interactive deadline shows a recoverable prompt instead of failing:
// "keep waiting" re-arms the deadline (the prompt returns), and "stop" stops
// the container and surfaces the timeout.
func TestStartupMonitorAwait_InteractivePromptKeepWaitingThenStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid").Return(true, nil).AnyTimes()
	mockRT.EXPECT().Stop(gomock.Any(), "cid").Return(nil)

	prompts := make(chan output.UserInputRequestEvent, 2)
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			prompts <- req
		}
	})

	go func() {
		for i, key := range []string{"w", "s"} {
			select {
			case req := <-prompts:
				req.ResponseCh <- output.InputResponse{SelectedKey: key}
			case <-time.After(5 * time.Second):
				t.Errorf("prompt %d never appeared", i+1)
				return
			}
		}
	}()

	exitCh := make(chan runtime.ExitResult)
	monitor := newStartupMonitor(mockRT, sink, nil, 50*time.Millisecond, true)
	err := monitor.await(context.Background(), "cid", unreachableHealthURL(t), exitCh)

	var timeoutErr *startupTimeoutError
	require.ErrorAs(t, err, &timeoutErr)
	assert.True(t, timeoutErr.stopped, "choosing stop at the prompt must be recorded on the error")
}

func TestStartupMonitorAwait_ReturnsExitCodeFromExitCh(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// Running on the first probe; the exit arrives via exitCh.
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid").Return(true, nil).AnyTimes()

	sink := output.NewPlainSink(io.Discard)

	monitor := newStartupMonitor(mockRT, sink, nil, time.Minute, false)
	err := monitor.await(context.Background(), "cid", unreachableHealthURL(t), exitResultChan(runtime.ExitResult{ExitCode: 42}))

	var exitErr *containerExitedError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 42, exitErr.exitCode)
}

func TestStartupMonitorAwait_WaitErrorFallsBackToPoll(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// First probe: running. After the exitCh error nils the channel, the poll
	// sees the container gone and reports an unknown exit code.
	gomock.InOrder(
		mockRT.EXPECT().IsRunning(gomock.Any(), "cid").Return(true, nil),
		mockRT.EXPECT().IsRunning(gomock.Any(), "cid").Return(false, nil),
	)

	sink := output.NewPlainSink(io.Discard)

	monitor := newStartupMonitor(mockRT, sink, nil, time.Minute, false)
	err := monitor.await(context.Background(), "cid", unreachableHealthURL(t), exitResultChan(runtime.ExitResult{ExitCode: -1, Err: errors.New("wait failed")}))

	var exitErr *containerExitedError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, -1, exitErr.exitCode, "an exit detected only by polling has an unknown code")
}

func TestResolveStartupTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		interactive bool
		want        time.Duration
	}{
		{"zero interactive uses short default", 0, true, defaultStartupTimeoutInteractive},
		{"zero non-interactive uses long default", 0, false, defaultStartupTimeoutNonInteractive},
		{"explicit wins interactive", 5 * time.Second, true, 5 * time.Second},
		{"explicit wins non-interactive", 5 * time.Second, false, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveStartupTimeout(tt.timeout, tt.interactive))
		})
	}
}

func TestStartContainers_ExitedEmitsErrorAndTelemetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:latest",
		Name:          "localstack-aws",
		EmulatorType:  config.EmulatorAWS,
		Tag:           "latest",
		Port:          "1", // unreachable port so the health GET never connects
		ContainerPort: "4566/tcp",
		HealthPath:    "/_localstack/health",
	}
	const containerID = "abc123"
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, exitResultChan(runtime.ExitResult{ExitCode: 3}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), containerID, gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(true, nil).AnyTimes()
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return("boom: fatal error\n", nil).AnyTimes()

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{}, time.Minute, false, false)
	tel.Close()

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error should be silent since an ErrorEvent was emitted")
	got := out.String()
	assert.Contains(t, got, "exited unexpectedly (exit code 3)")
	assert.Contains(t, got, "boom: fatal error", "the log tail should be surfaced in the summary")

	select {
	case ev := <-capturedEvents:
		payload, ok := ev["payload"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, telemetry.LifecycleStartError, payload["event_type"])
		assert.Equal(t, telemetry.ErrCodeStartFailed, payload["error_code"])
	default:
		t.Fatal("no telemetry event received")
	}
}

func TestStartContainers_TimeoutEmitsErrorAndTelemetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:latest",
		Name:          "localstack-aws",
		EmulatorType:  config.EmulatorAWS,
		Tag:           "latest",
		Port:          "1", // unreachable port so the health GET never connects
		ContainerPort: "4566/tcp",
		HealthPath:    "/_localstack/health",
	}
	const containerID = "abc123"
	// exitCh never fires; the container stays running but never becomes healthy.
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, (<-chan runtime.ExitResult)(make(chan runtime.ExitResult)), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), containerID, gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(true, nil).AnyTimes()
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return("still booting...\n", nil).AnyTimes()

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{}, 50*time.Millisecond, false, false)
	tel.Close()

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	got := out.String()
	assert.Contains(t, got, "did not become ready within 50ms")
	assert.Contains(t, got, "lstk logs")
	assert.Contains(t, got, "lstk stop")

	select {
	case ev := <-capturedEvents:
		payload, ok := ev["payload"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, telemetry.ErrCodeStartTimeout, payload["error_code"])
	default:
		t.Fatal("no telemetry event received")
	}
}

func TestLastLogLines(t *testing.T) {
	assert.Equal(t, "", lastLogLines("", 5))
	assert.Equal(t, "a\nb", lastLogLines("a\nb\n", 5))
	assert.Equal(t, "d\ne", lastLogLines("a\nb\nc\nd\ne", 2))
	assert.Equal(t, "only", lastLogLines("only", 3))
}

func TestPullImages_ReusesLocalImageWhenPresent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:3.5.0",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "3.5.0",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	// No PullImage call expected when the image is already present.

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	pulled, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, false)

	require.NoError(t, err)
	assert.False(t, pulled[c.Name], "telemetry must not count a reused image as pulled")
	assert.Contains(t, out.String(), "Using local image localstack/localstack-pro:3.5.0")
}

func TestPullImages_PullsWhenImageMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:3.5.0",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "3.5.0",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(false, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, progress chan<- runtime.PullProgress) error {
			close(progress)
			return nil
		})

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	pulled, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, false)

	require.NoError(t, err)
	assert.True(t, pulled[c.Name], "a freshly pulled image must be counted as pulled")
	assert.Contains(t, out.String(), "Pulled localstack/localstack-pro:3.5.0")
}

// recordingSink captures emitted events and optionally reacts to a
// PullSkippableEvent (mimicking the TUI binding ESC) by invoking onSkippable.
type recordingSink struct {
	mu          sync.Mutex
	events      []output.Event
	onSkippable func(output.PullSkippableEvent)
}

func (s *recordingSink) Emit(e output.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	cb := s.onSkippable
	s.mu.Unlock()
	if ev, ok := e.(output.PullSkippableEvent); ok && cb != nil {
		cb(ev)
	}
}

func (s *recordingSink) messageTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var texts []string
	for _, e := range s.events {
		if m, ok := e.(output.MessageEvent); ok {
			texts = append(texts, m.Text)
		}
	}
	return texts
}

func (s *recordingSink) sawSkippable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if _, ok := e.(output.PullSkippableEvent); ok {
			return true
		}
	}
	return false
}

func (s *recordingSink) sawError() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if _, ok := e.(output.ErrorEvent); ok {
			return true
		}
	}
	return false
}

// PRO-324: ESC during a floating-tag pull cancels the pull and falls back to the
// already-present local image without erroring out.
func TestPullImages_EscSkipsPullAndUsesLocal(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:latest",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "latest",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, progress chan<- runtime.PullProgress) error {
			// Report real layer download so the pull becomes skippable, then block
			// until the domain cancels in response to the ESC signal.
			progress <- runtime.PullProgress{LayerID: "layer1", Status: "Downloading", Current: 10, Total: 100}
			<-ctx.Done()
			close(progress)
			return ctx.Err()
		})

	sink := &recordingSink{}
	sink.onSkippable = func(ev output.PullSkippableEvent) { ev.SkipCh <- struct{}{} }

	pulled, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, true)

	require.NoError(t, err)
	assert.False(t, pulled[c.Name], "a skipped pull must not be counted as pulled")
	assert.True(t, sink.sawSkippable(), "a skippable pull event should be emitted once download begins")
	assert.False(t, sink.sawError(), "skipping the pull must not surface an error")
	assert.Contains(t, strings.Join(sink.messageTexts(), "\n"), "Keeping current local image localstack/localstack-pro:latest")
}

// PRO-324: a pull that fails with a local copy present falls back automatically
// (e.g. offline at a booth) instead of aborting the start.
func TestPullImages_PullErrorFallsBackToLocal(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:latest",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "latest",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, progress chan<- runtime.PullProgress) error {
			close(progress)
			return errors.New("no route to host")
		})

	sink := &recordingSink{}

	pulled, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, false)

	require.NoError(t, err, "a failed pull with a local image present must not be fatal")
	assert.False(t, pulled[c.Name], "a fall-back image must not be counted as pulled")
	assert.False(t, sink.sawError(), "the fall-back must not surface an ErrorEvent")
	assert.Contains(t, strings.Join(sink.messageTexts(), "\n"), "Could not pull localstack/localstack-pro:latest")
}

// PRO-324: a failed pull with no local copy stays fatal (preserves prior behavior).
func TestPullImages_PullErrorWithoutLocalIsFatal(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:latest",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "latest",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(false, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, progress chan<- runtime.PullProgress) error {
			close(progress)
			return errors.New("manifest unknown")
		})

	sink := &recordingSink{}

	_, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, true)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error should be silent since an ErrorEvent was emitted")
	assert.True(t, sink.sawError(), "a fatal pull failure must surface an ErrorEvent")
}

// PRO-324: in non-interactive mode no skippable-pull affordance is offered, even
// when a local copy exists; the pull just proceeds.
func TestPullImages_NonInteractiveNeverSkippable(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:latest",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "latest",
	}

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, progress chan<- runtime.PullProgress) error {
			progress <- runtime.PullProgress{LayerID: "layer1", Status: "Downloading", Current: 10, Total: 100}
			close(progress)
			return nil
		})

	sink := &recordingSink{}

	pulled, err := pullImages(context.Background(), mockRT, sink, mockTel, []runtime.ContainerConfig{c}, false)

	require.NoError(t, err)
	assert.True(t, pulled[c.Name], "a completed pull must be counted as pulled")
	assert.False(t, sink.sawSkippable(), "non-interactive mode must never offer to skip the pull")
}

// PRO-324: cancelling the parent context (e.g. Ctrl+C) during a pull is a
// deliberate abort, not a pull failure — it must propagate as context.Canceled
// rather than being swallowed by the local-image fall-back.
func TestPullImages_ParentCancelPropagatesNotFallBack(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockTel := telemetry.New("", true)

	c := runtime.ContainerConfig{
		Image:        "localstack/localstack-pro:latest",
		Name:         "localstack-aws",
		EmulatorType: config.EmulatorAWS,
		Tag:          "latest",
	}

	ctx, cancel := context.WithCancel(context.Background())

	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	mockRT.EXPECT().PullImage(gomock.Any(), c.Image, gomock.Any()).
		DoAndReturn(func(pullCtx context.Context, _ string, progress chan<- runtime.PullProgress) error {
			// Simulate Ctrl+C: the parent ctx is cancelled mid-pull, which cancels
			// the derived pullCtx; the runtime then returns its context error.
			cancel()
			<-pullCtx.Done()
			close(progress)
			return pullCtx.Err()
		})

	sink := &recordingSink{}

	_, err := pullImages(ctx, mockRT, sink, mockTel, []runtime.ContainerConfig{c}, false)

	require.Error(t, err, "a cancelled pull must not be swallowed as a successful fall-back")
	assert.True(t, errors.Is(err, context.Canceled), "parent cancellation must propagate")
	assert.NotContains(t, strings.Join(sink.messageTexts(), "\n"), "using the local image",
		"cancellation must not emit the offline fall-back message")
}

func TestValidateLicense_DefersOnServerError(t *testing.T) {
	// A 5xx (or 407, ...) from the license server is an outage, not a verdict on
	// the license — the pre-flight must degrade to container-side validation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
		Image:        "localstack/localstack-pro:2026.4",
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	_, err := validateLicense(context.Background(), sink, opts, c, "tok", filepath.Join(t.TempDir(), "license.json"))

	require.NoError(t, err, "a license server outage must not block the start")
	assert.Contains(t, out.String(), "unexpected response (HTTP 502)")
}

func TestValidateLicense_RemovesCachedLicenseOnRejection(t *testing.T) {
	// A definitive rejection invalidates the cached license: a later start whose
	// pre-flight is skipped (e.g. image already local) must not keep mounting the
	// stale copy (DEVX-658).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	licenseFilePath := filepath.Join(t.TempDir(), "license.json")
	require.NoError(t, os.WriteFile(licenseFilePath, []byte(`{"license":"stale"}`), 0600))

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	c := runtime.ContainerConfig{
		EmulatorType: config.EmulatorAWS,
		ProductName:  "localstack-pro",
		Tag:          "2026.4",
		Image:        "localstack/localstack-pro:2026.4",
	}

	_, err := validateLicense(context.Background(), output.NewPlainSink(io.Discard), opts, c, "tok", licenseFilePath)

	require.Error(t, err, "a server rejection must remain fatal")
	assert.NoFileExists(t, licenseFilePath, "the stale cached license must be removed on a definitive rejection")
}

func TestLogsIndicateLicenseFailure(t *testing.T) {
	cases := []struct {
		name string
		logs string
		want bool
	}{
		{"activation failure", "ERROR: License activation failed! Please check your auth token.", true},
		{"invalid license", "The License is invalid or has expired", true},
		{"license mentioned without failure", "Checking license... OK\nReady.", false},
		{"unrelated crash", "panic: something exploded", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, logsIndicateLicenseFailure(tc.logs))
		})
	}
}

func TestStartWithLicenseRetry_RefreshesStaleCachedLicense(t *testing.T) {
	// The joel scenario from DEVX-658: the pre-flight was skipped (image already
	// local), so a stale cached license.json was mounted and the emulator exited
	// with a license failure. The start must drop the cache, fetch a fresh
	// license, and retry once — without a manual `lstk logout`.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	healthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthSrv.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(healthSrv.URL, "http://"))
	require.NoError(t, err)

	licenseFilePath := filepath.Join(t.TempDir(), "license.json")
	require.NoError(t, os.WriteFile(licenseFilePath, []byte(`{"license":"stale"}`), 0600))

	var licenseHits int32
	licSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&licenseHits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"license_type":"enterprise","license":"fresh"}`))
	}))
	defer licSrv.Close()

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:2026.4",
		Name:          "localstack-aws",
		EmulatorType:  config.EmulatorAWS,
		ProductName:   "localstack-pro",
		Tag:           "2026.4",
		Port:          port,
		ContainerPort: "4566/tcp",
		HealthPath:    "/health",
	}

	// First attempt: the container exits with a license failure.
	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return("cid1cid1cid1", exitResultChan(runtime.ExitResult{ExitCode: 1}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), "cid1cid1cid1", gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid1cid1cid1").Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), "cid1cid1cid1", 20).Return("License activation failed: the license is invalid or expired", nil)

	// Second attempt succeeds (the health endpoint responds 200).
	var secondStart runtime.ContainerConfig
	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, cfg runtime.ContainerConfig) (string, <-chan runtime.ExitResult, error) {
		secondStart = cfg
		return "cid2cid2cid2", make(chan runtime.ExitResult), nil
	})
	mockRT.EXPECT().StreamLogs(gomock.Any(), "cid2cid2cid2", gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid2cid2cid2").Return(true, nil).AnyTimes()

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(licSrv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err = startWithLicenseRetry(context.Background(), mockRT, sink, opts, false, []runtime.ContainerConfig{c}, map[string]bool{}, "tok", licenseFilePath, false)

	require.NoError(t, err, "the retry with a refreshed license must succeed; output: %s", out.String())
	assert.Equal(t, int32(1), atomic.LoadInt32(&licenseHits), "the retry must fetch a fresh license exactly once")
	data, readErr := os.ReadFile(licenseFilePath)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "fresh", "the cached license must be replaced by the freshly fetched one")

	var licenseBinds int
	for _, b := range secondStart.Binds {
		if b.ContainerPath == licenseMountPath {
			licenseBinds++
			assert.Equal(t, licenseFilePath, b.HostPath)
		}
	}
	assert.Equal(t, 1, licenseBinds, "the retry must mount exactly one refreshed license file")
	assert.Contains(t, out.String(), "refreshing the cached license and retrying")
}

func TestStartWithLicenseRetry_NoRetryWhenPreflightRefreshedLicense(t *testing.T) {
	// When this run already fetched a fresh license, a startup license failure is
	// a real verdict — refetching the same license again would loop for nothing.
	// The failure renders through the regular startup error path instead.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	licenseFilePath := filepath.Join(t.TempDir(), "license.json")
	require.NoError(t, os.WriteFile(licenseFilePath, []byte(`{"license":"fresh"}`), 0600))

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:2026.4",
		Name:          "localstack-aws",
		EmulatorType:  config.EmulatorAWS,
		ProductName:   "localstack-pro",
		Tag:           "2026.4",
		Port:          "4566",
		ContainerPort: "4566/tcp",
		HealthPath:    "/health",
	}

	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return("cid1cid1cid1", exitResultChan(runtime.ExitResult{ExitCode: 1}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), "cid1cid1cid1", gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid1cid1cid1").Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), "cid1cid1cid1", 20).Return("License activation failed", nil)

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient("http://127.0.0.1:1", log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startWithLicenseRetry(context.Background(), mockRT, sink, opts, false, []runtime.ContainerConfig{c}, map[string]bool{}, "tok", licenseFilePath, true)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "the failure must be rendered by the regular startup error path")
	assert.NotContains(t, out.String(), "refreshing the cached license", "no retry when the license was freshly fetched this run")
	assert.FileExists(t, licenseFilePath, "a freshly validated license must not be dropped")
}

func TestStartWithLicenseRetry_NoRetryWithoutCachedLicense(t *testing.T) {
	// With no cached license mounted, a startup license failure cannot be a stale
	// cache problem — the failure renders through the regular startup error path.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:         "localstack/localstack-pro:2026.4",
		Name:          "localstack-aws",
		EmulatorType:  config.EmulatorAWS,
		ProductName:   "localstack-pro",
		Tag:           "2026.4",
		Port:          "4566",
		ContainerPort: "4566/tcp",
		HealthPath:    "/health",
	}

	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return("cid1cid1cid1", exitResultChan(runtime.ExitResult{ExitCode: 1}), nil)
	mockRT.EXPECT().StreamLogs(gomock.Any(), "cid1cid1cid1", gomock.Any(), true, "all").Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "cid1cid1cid1").Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), "cid1cid1cid1", 20).Return("License activation failed", nil)

	opts := StartOptions{
		PlatformClient: api.NewPlatformClient("http://127.0.0.1:1", log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startWithLicenseRetry(context.Background(), mockRT, sink, opts, false, []runtime.ContainerConfig{c}, map[string]bool{}, "tok", filepath.Join(t.TempDir(), "license.json"), false)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "the failure must be rendered by the regular startup error path")
	assert.NotContains(t, out.String(), "refreshing the cached license")
}

// TestStart_SecondLicenseRejectionAfterReloginRendersErrorEvent covers a gap in
// DEVX-658: if the freshly re-logged-in token is rejected too (the license
// server's answer didn't change), the second rejection must render through the
// same styled ErrorEvent as the first — not bypass the sink and fall through to
// the top-level "Error: %v" fallback (cmd/root.go), which is reserved for
// failures no sink was available to render.
func TestStart_SecondLicenseRejectionAfterReloginRendersErrorEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const authReqID = "second-rejection-auth-req"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/request":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": authReqID, "code": "TEST123", "exchange_token": "exchange-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/request/"+authReqID:
			_ = json.NewEncoder(w).Encode(map[string]bool{"confirmed": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/request/"+authReqID+"/exchange":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": authReqID, "auth_token": "Bearer test-bearer"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/license/credentials":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "fresh-token"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/license/request":
			// Every license check is rejected — both the stale token on the first
			// attempt and the freshly re-logged-in one on the retry.
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().SocketPath().Return("").AnyTimes()
	mockRT.EXPECT().IsRunning(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	mockRT.EXPECT().ImageExists(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()

	var mu sync.Mutex
	var out bytes.Buffer
	sink := output.SinkFunc(func(event output.Event) {
		mu.Lock()
		if line, ok := output.FormatEventLine(event); ok {
			out.WriteString(line + "\n")
		}
		mu.Unlock()
		// Auto-answer every prompt (the re-login confirmation, then the login
		// flow's "press any key" completion prompt) as if the user pressed enter.
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{SelectedKey: "enter"}
		}
	})

	opts := StartOptions{
		PlatformClient:   api.NewPlatformClient(mockServer.URL, log.Nop()),
		WebAppURL:        mockServer.URL,
		AuthToken:        "stale-token-predating-license-purchase",
		ForceFileKeyring: true,
		Logger:           log.Nop(),
		Telemetry:        telemetry.New("", true),
		AuthOptions:      []auth.Option{auth.WithBrowserOpener(func(string) error { return nil })},
		Containers: []config.ContainerConfig{
			{Type: config.EmulatorAWS, Port: "48213", Tag: "2026.4"},
		},
	}

	_, err := Start(context.Background(), mockRT, sink, opts, true)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "a second rejection must render through the sink, not the raw stderr fallback")
	mu.Lock()
	got := out.String()
	mu.Unlock()
	assert.Contains(t, got, "License validation failed", "the second rejection must render the same actionable error as the first")
	assert.Contains(t, got, "lstk logout && lstk login")
}

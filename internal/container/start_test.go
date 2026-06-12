package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/localstack/lstk/internal/caller"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

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

	require.Len(t, ports, 51)
	assert.Equal(t, "443", ports[0].ContainerPort)
	assert.Equal(t, "443", ports[0].HostPort)
	assert.Equal(t, "4510", ports[1].ContainerPort)
	assert.Equal(t, "4510", ports[1].HostPort)
	assert.Equal(t, "4559", ports[50].ContainerPort)
	assert.Equal(t, "4559", ports[50].HostPort)
}

func TestFilterHostEnv(t *testing.T) {
	input := []string{
		"CI=true",
		"LOCALSTACK_DISABLE_EVENTS=1",
		"LOCALSTACK_API_ENDPOINT=https://example.test",
		"LOCALSTACK_AUTH_TOKEN=host-token",
		"LOCALSTACK_PERSISTENCE=1",
		"PATH=/usr/bin",
		"HOME=/home/user",
		"CI_PIPELINE=foo",
	}

	got := filterHostEnv(input)

	assert.Contains(t, got, "CI=true")
	assert.Contains(t, got, "LOCALSTACK_DISABLE_EVENTS=1")
	assert.Contains(t, got, "LOCALSTACK_API_ENDPOINT=https://example.test")
	assert.Contains(t, got, "LOCALSTACK_PERSISTENCE=1")
	assert.NotContains(t, got, "LOCALSTACK_AUTH_TOKEN=host-token",
		"host LOCALSTACK_AUTH_TOKEN must be filtered so it cannot overwrite the lstk-resolved token")
	assert.NotContains(t, got, "PATH=/usr/bin")
	assert.NotContains(t, got, "HOME=/home/user")
	assert.NotContains(t, got, "CI_PIPELINE=foo", "only exact CI= must be forwarded, not CI_*")
}

func TestAgentEnv(t *testing.T) {
	assert.Equal(t, []string{"AI_AGENT=claude-code"},
		agentEnv(caller.Classification{AgentIdentity: "claude-code"}),
		"a detected agent is forwarded as AI_AGENT")
	assert.Nil(t, agentEnv(caller.Classification{Interactive: true}),
		"a human start forwards no AI_AGENT")
	assert.Nil(t, agentEnv(caller.Classification{CIIdentity: "github-actions"}),
		"CI without an agent is carried by the forwarded CI variable, not AI_AGENT")
	assert.Equal(t, []string{"AI_AGENT=claude-code"},
		agentEnv(caller.Classification{AgentIdentity: "claude-code", CIIdentity: "github-actions"}),
		"an agent running inside CI still forwards its AI_AGENT identity")
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
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return(licenseLog, nil)

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{})
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
	mockRT.EXPECT().Start(gomock.Any(), c).Return(containerID, nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), containerID).Return(false, nil)
	mockRT.EXPECT().Logs(gomock.Any(), containerID, 20).Return(licenseLog, nil)

	tel, capturedEvents := newCapturingTelClient(t)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := startContainers(context.Background(), mockRT, sink, tel, []runtime.ContainerConfig{c}, map[string]bool{})
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

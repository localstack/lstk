package snapshot

//go:generate mockgen -source=load.go -destination=mock_load_client_test.go -package=snapshot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const (
	MergeStrategyAccountRegion = "account-region-merge"
	MergeStrategyOverwrite     = "overwrite"
	MergeStrategyService       = "service-merge"
)

var ErrIncompatibleSnapshot = errors.New("snapshot is incompatible with the running LocalStack version")

// ErrPodVersionNotFound indicates the requested version of an existing cloud
// snapshot does not exist. The emulator reports it with a message naming the
// highest available version, which is passed through to the user verbatim.
var ErrPodVersionNotFound = errors.New("snapshot version not found")

// ErrInvalidSnapshotFile indicates the source could not be read as a snapshot
// (e.g. a non-snapshot file was passed). It deliberately hides the underlying
// archive format from the user-facing message.
var ErrInvalidSnapshotFile = errors.New("not a valid snapshot file")

// ErrSnapshotFeatureUnavailable indicates the emulator's license lacks the
// paid entitlement for snapshots (branded "Cloud Pods", but required for
// local-file and S3-remote saves too, not just platform pods). Its
// /_localstack/pods* routes are then never registered, so the emulator
// replies with a bare, empty-body 404 — see isFeatureUnavailableResponse.
//
// Kept feature-neutral since aws.Client.ResetState is shared with `lstk
// reset`, which surfaces this text directly; snapshot-specific wording lives
// in emitFeatureUnavailableError instead.
var ErrSnapshotFeatureUnavailable = errors.New("feature not available on this plan")

// emitFeatureUnavailableError renders the shared "requires a paid plan" message
// and returns the silent error the top-level handler expects. Every snapshot
// operation funnels through here so the wording and CTAs live in one place.
func emitFeatureUnavailableError(sink output.Sink) error {
	sink.Emit(output.ErrorEvent{
		Title:   "Snapshots require a paid LocalStack plan",
		Summary: "Your plan does not include the snapshot feature.",
		Actions: []output.ErrorAction{
			{Label: "Compare plans:", Value: "https://www.localstack.cloud/pricing"},
		},
	})
	return output.NewSilentError(ErrSnapshotFeatureUnavailable)
}

func ValidateMergeStrategy(strategy string) error {
	switch strategy {
	case MergeStrategyAccountRegion, MergeStrategyOverwrite, MergeStrategyService:
		return nil
	default:
		return fmt.Errorf("unknown merge strategy %q: use overwrite, account-region-merge, or service-merge", strategy)
	}
}

// Starter is called to auto-start the emulator when none is running.
type Starter func(ctx context.Context, sink output.Sink) error

// LocalLoadClient is satisfied by aws.Client.
type LocalLoadClient interface {
	// ImportState posts a zip to /_localstack/pods[?merge=strategy] and streams
	// the NDJSON response. strategy is passed as-is; empty means server default.
	ImportState(ctx context.Context, host string, src io.Reader, strategy string) error
	// ResetState wipes all running state via POST /_localstack/state/reset.
	// Used to implement overwrite client-side before importing.
	ResetState(ctx context.Context, host string) error
}

// PodLoader is satisfied by aws.Client.
type PodLoader interface {
	// LoadPodSnapshot issues PUT /_localstack/pods/{name}?merge=strategy and
	// streams the NDJSON response. version 0 loads the pod's latest version.
	LoadPodSnapshot(ctx context.Context, host, podName string, version int, authToken, strategy string) ([]string, error)
}

// load is the shared entry point for both LoadLocal and LoadPod.
// It checks runtime health, auto-starts the emulator if needed, then runs do().
func load(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, sink output.Sink, starter Starter, spinnerText string, onSuccess func(), do func() error) (retErr error) {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	emitExperimentalWarning(containers, sink)

	runningContainers, err := container.RunningEmulators(ctx, rt, containers)
	if err != nil {
		return fmt.Errorf("checking emulator status: %w", err)
	}

	if len(runningContainers) == 0 {
		if starter == nil {
			sink.Emit(output.ErrorEvent{
				Title: "LocalStack is not running",
				Actions: []output.ErrorAction{
					{Label: "Start LocalStack:", Value: "lstk"},
					{Label: "See help:", Value: "lstk -h"},
				},
			})
			return output.NewSilentError(fmt.Errorf("LocalStack is not running"))
		}
		if err := starter(ctx, sink); err != nil {
			return err
		}
	}

	sink.Emit(output.SpinnerStart(spinnerText))
	defer func() {
		sink.Emit(output.SpinnerStop())
		if retErr == nil {
			onSuccess()
		}
	}()

	err = do()
	if errors.Is(err, ErrSnapshotFeatureUnavailable) {
		return emitFeatureUnavailableError(sink)
	}
	if errors.Is(err, ErrIncompatibleSnapshot) {
		sink.Emit(output.ErrorEvent{
			Title:   "Could not load snapshot",
			Summary: "Snapshot is incompatible with the running LocalStack version",
		})
		return output.NewSilentError(err)
	}
	if errors.Is(err, ErrInvalidSnapshotFile) {
		sink.Emit(output.ErrorEvent{
			Title:   "Could not load snapshot",
			Summary: "This file is not a valid snapshot",
		})
		return output.NewSilentError(err)
	}
	if errors.Is(err, ErrPodNotFound) {
		sink.Emit(output.ErrorEvent{
			Title:   "Could not load snapshot",
			Summary: "Snapshot was not found on the LocalStack platform",
			Actions: []output.ErrorAction{
				{Label: "List your snapshots:", Value: "lstk snapshot list"},
			},
		})
		return output.NewSilentError(err)
	}
	return err
}

func LoadLocal(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client LocalLoadClient, host, src, strategy string, starter Starter, sink output.Sink) error {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	return load(ctx, rt, containers, sink, starter,
		"Loading snapshot...",
		func() {
			sink.Emit(output.SnapshotLoadedEvent{Source: displayPath(src, cwd, home)})
		},
		func() error {
			// overwrite is handled client-side: reset running state, then import
			// with the server default (account-region-merge on clean state = overwrite).
			if strategy == MergeStrategyOverwrite {
				if err := client.ResetState(ctx, host); err != nil {
					return fmt.Errorf("reset state: %w", err)
				}
				strategy = ""
			}

			f, err := os.Open(src)
			if err != nil {
				return fmt.Errorf("open snapshot: %w", err)
			}
			defer func() { _ = f.Close() }()

			return client.ImportState(ctx, host, f, strategy)
		},
	)
}

// LoadPod loads a platform-hosted cloud snapshot. version 0 loads the pod's
// latest version; a non-zero version pins the load to that specific one.
func LoadPod(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, loader PodLoader, host, podName string, version int, authToken, strategy string, starter Starter, sink output.Sink) error {
	if authToken == "" {
		return fmt.Errorf("pod snapshots require authentication — set LOCALSTACK_AUTH_TOKEN or run %q", "lstk login")
	}

	spinnerText := fmt.Sprintf("Loading snapshot from pod %q...", podName)
	if version > 0 {
		spinnerText = fmt.Sprintf("Loading snapshot from pod %q (version %d)...", podName, version)
	}

	var services []string
	err := load(ctx, rt, containers, sink, starter,
		spinnerText,
		func() {
			sink.Emit(output.SnapshotLoadedEvent{
				Source:   PodRef(podName, version),
				Services: services,
			})
		},
		func() error {
			var err error
			services, err = loader.LoadPodSnapshot(ctx, host, podName, version, authToken, strategy)
			return err
		},
	)
	// Handled here rather than in load(), which is shared with the local and S3
	// paths and has no pod name to point the user at.
	if errors.Is(err, ErrPodVersionNotFound) {
		return emitPodVersionNotFound(err, podName, "Could not load snapshot", sink)
	}
	return err
}

// emitPodVersionNotFound renders a missing-version failure. The emulator's own
// message already names the highest available version, so it is surfaced verbatim
// as the summary rather than being restated.
func emitPodVersionNotFound(err error, podName, title string, sink output.Sink) error {
	sink.Emit(output.ErrorEvent{
		Title:   title,
		Summary: strings.TrimPrefix(err.Error(), ErrPodVersionNotFound.Error()+": "),
		Actions: []output.ErrorAction{
			{Label: "List available versions:", Value: "lstk snapshot versions pod:" + podName},
		},
	})
	return output.NewSilentError(err)
}

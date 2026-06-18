package snapshot

//go:generate mockgen -source=load.go -destination=mock_load_client_test.go -package=snapshot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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

// ErrInvalidSnapshotFile indicates the source could not be read as a snapshot
// (e.g. a non-snapshot file was passed). It deliberately hides the underlying
// archive format from the user-facing message.
var ErrInvalidSnapshotFile = errors.New("not a valid snapshot file")

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
	// streams the NDJSON response.
	LoadPodSnapshot(ctx context.Context, host, podName, authToken, strategy string) ([]string, error)
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

func LoadPod(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, loader PodLoader, host, podName, authToken, strategy string, starter Starter, sink output.Sink) error {
	if authToken == "" {
		return fmt.Errorf("pod snapshots require authentication — set LOCALSTACK_AUTH_TOKEN or run %q", "lstk login")
	}

	var services []string
	return load(ctx, rt, containers, sink, starter,
		fmt.Sprintf("Loading snapshot from pod %q...", podName),
		func() {
			sink.Emit(output.SnapshotLoadedEvent{
				Source:   "pod:" + podName,
				Services: services,
			})
		},
		func() error {
			var err error
			services, err = loader.LoadPodSnapshot(ctx, host, podName, authToken, strategy)
			return err
		},
	)
}

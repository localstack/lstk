package volume

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/docker/go-units"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

func Clear(ctx context.Context, sink output.Sink, containers []config.ContainerConfig, force bool) error {
	type target struct {
		name string
		path string
		size int64
	}

	var targets []target
	for _, c := range containers {
		volumeDir, err := c.VolumeDir()
		if err != nil {
			return err
		}
		size, err := dirSize(volumeDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read volume directory %s: %w", volumeDir, err)
		}
		targets = append(targets, target{name: c.DisplayName(), path: volumeDir, size: size})
	}

	for _, t := range targets {
		output.EmitInfo(sink, fmt.Sprintf("%s: %s (%s)", t.name, t.path, units.BytesSize(float64(t.size))))
	}

	if !force {
		responseCh := make(chan output.InputResponse, 1)
		output.EmitUserInputRequest(sink, output.UserInputRequestEvent{
			Prompt: "Clear volume data? This cannot be undone",
			Options: []output.InputOption{
				{Key: "y", Label: "Yes"},
				{Key: "n", Label: "NO"},
			},
			ResponseCh: responseCh,
		})

		select {
		case resp := <-responseCh:
			if resp.Cancelled || resp.SelectedKey != "y" {
				output.EmitNote(sink, "Cancelled")
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	for _, t := range targets {
		if err := clearDir(t.path); err != nil {
			return fmt.Errorf("failed to clear %s: %w", t.path, err)
		}
	}

	output.EmitSuccess(sink, "Volume data cleared")
	return nil
}

func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}


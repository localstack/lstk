package components

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: pull_containerd_python.jsonl is a real Docker Desktop 4.92
// (containerd store) pull of python:3.13-slim; pull_overlay2_synthetic.jsonl
// is hand-written in the classic overlay2 format.
func pullStreamFixtures() []string {
	return []string{
		"testdata/pull_containerd_python.jsonl",
		"testdata/pull_overlay2_synthetic.jsonl",
	}
}

// loadPullStream decodes a Docker /images/create stream like
// runtime.DockerRuntime.PullImage does.
func loadPullStream(t *testing.T, path string) []output.ProgressEvent {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var events []output.ProgressEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var msg struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		require.NoError(t, json.Unmarshal(sc.Bytes(), &msg))
		events = append(events, output.ProgressEvent{
			LayerID: msg.ID,
			Status:  msg.Status,
			Current: msg.ProgressDetail.Current,
			Total:   msg.ProgressDetail.Total,
		})
	}
	require.NoError(t, sc.Err())
	return events
}

func TestPullProgress_ReplayBarNeverMovesBackwardOrFreezes(t *testing.T) {
	for _, fixture := range pullStreamFixtures() {
		t.Run(fixture, func(t *testing.T) {
			p := NewPullProgress().Show("image")
			shown, estimate := 0.0, 0.0
			var trajectory []string
			for i, e := range loadPullStream(t, fixture) {
				p, _ = p.SetProgress(e)
				target := p.bar.Percent() // what the bar animates towards
				if target < shown {
					t.Errorf("event %d (%s %s): bar moved backward %.1f%% -> %.1f%%",
						i, e.Status, e.LayerID, shown*100, target*100)
				}
				if next := p.aggregatePercent(); next > estimate+epsilon && target <= shown && shown < 1 {
					t.Errorf("event %d (%s %s): estimate rose but bar stayed at %.1f%%",
						i, e.Status, e.LayerID, shown*100)
				}
				shown, estimate = target, p.aggregatePercent()
				trajectory = append(trajectory, fmt.Sprintf("%.0f", target*100))
			}
			t.Logf("bar %%: %s", strings.Join(trajectory, " "))
		})
	}
}

func TestPullProgress_ReplayEndsComplete(t *testing.T) {
	for _, fixture := range pullStreamFixtures() {
		t.Run(fixture, func(t *testing.T) {
			p := NewPullProgress().Show("image")
			for _, e := range loadPullStream(t, fixture) {
				p, _ = p.SetProgress(e)
			}
			assert.InDelta(t, 1.0, p.bar.Percent(), 0.001)
			total, done := p.layerCounts()
			assert.Equal(t, total, done, "all counted layers should be done")
			assert.Contains(t, p.View(), "Complete")
		})
	}
}

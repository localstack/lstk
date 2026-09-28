package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullProgress_InitiallyHidden(t *testing.T) {
	p := NewPullProgress()
	assert.False(t, p.Visible())
	assert.Equal(t, "", p.View())
}

func TestPullProgress_ShowMakesVisible(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("localstack/localstack-pro:latest")
	assert.True(t, p.Visible())
}

func TestPullProgress_HideMakesInvisible(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("localstack/localstack-pro:latest")
	p = p.Hide()
	assert.False(t, p.Visible())
	assert.Equal(t, "", p.View())
}

func TestPullProgress_AggregatesMultipleLayers(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 25, Total: 100})

	// 75 of 200 bytes downloaded, nothing extracted yet.
	assert.InDelta(t, downloadWeight*0.375, p.aggregatePercent(), 0.001)
}

func TestPullProgress_ViewShowsLayerCounts(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "ccc", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pull complete", Current: 0, Total: 0})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 50, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "ccc", Status: "Downloading", Current: 10, Total: 100})

	view := p.View()
	assert.Contains(t, view, "1/3")
	assert.Contains(t, view, "layers")
}

func TestPullProgress_ViewEmptyWithNoLayers(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")
	assert.Equal(t, "", p.View())
}

func TestPullProgress_DominantPhase(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})
	assert.True(t, strings.Contains(p.View(), "Downloading"))

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Extracting", Current: 50, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Extracting", Current: 10, Total: 100})
	assert.True(t, strings.Contains(p.View(), "Extracting"))
}

func TestPullProgress_PhaseOnlyMovesForward(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Pulling fs layer"})
	assert.Contains(t, p.View(), "Pulling")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Downloading", Current: 1, Total: 1000})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Download complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Extracting", Current: 1})
	assert.Contains(t, p.View(), "Downloading", "a big layer is still downloading")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Download complete"})
	assert.Contains(t, p.View(), "Extracting")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Pull complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Pull complete"})
	assert.Contains(t, p.View(), "Complete")
}

func TestPullProgress_AlreadyExistsLayersAreNotCounted(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Already exists"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 50, Total: 100})

	// The counter and the bar cover the same layers: only what is pulled.
	assert.Contains(t, p.View(), "0/1")
	assert.InDelta(t, downloadWeight*0.5, p.aggregatePercent(), 0.001)
}

func TestPullProgress_ShowResetsLayers(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image-a")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})

	p = p.Show("image-b")
	assert.True(t, p.Visible())
	assert.Equal(t, 0, len(p.layers))
}

func TestPullProgress_IgnoresEmptyLayerID(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "", Status: "Pulling from library/localstack"})
	assert.Equal(t, 0, len(p.layers))
}

// The tests below check the raw estimate, not the clamped display, so they
// fail if the per-layer handling regresses.

func TestPullProgress_StatusOnlyEventsDoNotResetBytes(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 100, Total: 100})
	before := p.aggregatePercent()

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Verifying Checksum"})
	assert.InDelta(t, before, p.aggregatePercent(), 0.001)
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Download complete"})
	assert.InDelta(t, downloadWeight, p.aggregatePercent(), 0.001, "download done, extraction pending")
}

func TestPullProgress_ExtractionRestartDoesNotMoveBackward(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 100, Total: 100})
	before := p.aggregatePercent()

	// overlay2: extraction bytes restart at 0.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Extracting", Current: 10, Total: 100})
	assert.GreaterOrEqual(t, p.aggregatePercent(), before)
	// containerd: extraction reports elapsed seconds without a total.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Extracting", Current: 1})
	assert.GreaterOrEqual(t, p.aggregatePercent(), before)

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pull complete"})
	assert.InDelta(t, 1.0, p.aggregatePercent(), 0.001)
}

func TestPullProgress_ExtractionUsesItsOwnTotal(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Download complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Extracting", Current: 5, Total: 10})

	assert.InDelta(t, downloadWeight+extractWeight*0.5, p.aggregatePercent(), 0.001)
}

func TestPullProgress_RetryRestartsLayerDownload(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 80, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Retrying in 5 seconds"})
	assert.InDelta(t, 0.0, p.aggregatePercent(), 0.001)

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 10, Total: 100})
	assert.InDelta(t, downloadWeight*0.1, p.aggregatePercent(), 0.001)
}

func TestPullProgress_SmallUnsizedLayerDoesNotInflateProgress(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Pulling fs layer"})
	// containerd finishes small blobs without ever reporting their size.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "small", Status: "Download complete"})
	assert.InDelta(t, 0.0, p.percent, 0.001, "nothing is known about the pull size yet")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Downloading", Current: 1, Total: 1000})
	assert.Less(t, p.percent, 0.01)
}

func TestPullProgress_BarKeepsMovingAfterLargeLayerSizeAppears(t *testing.T) {
	p := NewPullProgress().Show("image")
	for _, id := range []string{"a", "b", "c", "big"} {
		p, _ = p.SetProgress(output.ProgressEvent{LayerID: id, Status: "Pulling fs layer"})
	}
	for _, id := range []string{"a", "b", "c"} {
		p, _ = p.SetProgress(output.ProgressEvent{LayerID: id, Status: "Downloading", Current: 1, Total: 1})
		p, _ = p.SetProgress(output.ProgressEvent{LayerID: id, Status: "Pull complete"})
	}

	// big's size is learned only now and dwarfs the rest: the estimate drops.
	prev := p.percent
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Downloading", Current: 1, Total: 1000})
	assert.Less(t, p.aggregatePercent(), prev, "raw estimate drops")
	assert.GreaterOrEqual(t, p.percent, prev, "display never moves backward")

	for current := int64(100); current <= 1000; current += 100 {
		prev = p.percent
		p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Downloading", Current: current, Total: 1000})
		assert.Greater(t, p.percent, prev, "display keeps moving at %d/1000", current)
	}
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "big", Status: "Pull complete"})
	assert.InDelta(t, 1.0, p.percent, 0.001)
}

func TestPullProgress_HeaderAndConfigBlobsAreNotLayers(t *testing.T) {
	p := NewPullProgress().Show("image")
	// The header line carries the tag as its id.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "latest", Status: "Pulling from localstack/localstack-pro"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	// containerd config blob: downloaded, never announced or extracted.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "cfg", Status: "Downloading", Current: 5, Total: 10})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "cfg", Status: "Download complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pull complete"})

	assert.Contains(t, p.View(), "1/1")
	assert.InDelta(t, 1.0, p.percent, 0.001)
}

func TestPullProgress_UnannouncedBlobsCountWhenNoLayerIsAnnounced(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 50, Total: 100})
	assert.Contains(t, p.View(), "0/2")
	assert.InDelta(t, downloadWeight*0.5, p.aggregatePercent(), 0.001)

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Download complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Download complete"})
	assert.Contains(t, p.View(), "2/2")
	assert.InDelta(t, 1.0, p.percent, 0.001)
}

func TestPullProgress_LateAnnouncementKeepsCountedBlobs(t *testing.T) {
	p := NewPullProgress().Show("image")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 10, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 10, Total: 100})
	// A later event announces layers; already counted blobs must stay counted,
	// or the denominator shrinks and the bar jumps ahead.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "ccc", Status: "Already exists"})

	assert.Contains(t, p.View(), "0/2")
	assert.InDelta(t, downloadWeight*0.1, p.aggregatePercent(), 0.001)
}

func TestPullProgress_SizelessBlobBeforeAnnouncementIsIgnored(t *testing.T) {
	p := NewPullProgress().Show("image")
	// containerd may report the index before announcing layers.
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "idx", Status: "Download complete"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 40, Total: 100})

	assert.Contains(t, p.View(), "0/1")
	assert.InDelta(t, downloadWeight*0.4, p.percent, 0.001)
}

func TestPullProgress_ShowResetsPercent(t *testing.T) {
	p := NewPullProgress().Show("image-a")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})
	assert.Greater(t, p.percent, 0.0)

	p = p.Show("image-b")
	assert.InDelta(t, 0.0, p.percent, 0.001)
}

// runFrames plays the bar's animation until cmd stops scheduling frames.
func runFrames(t *testing.T, p PullProgress, cmd tea.Cmd) PullProgress {
	t.Helper()
	for i := 0; cmd != nil && i < 200; i++ {
		p, cmd = p.Update(cmd())
	}
	require.Nil(t, cmd, "animation did not settle")
	return p
}

func TestPullProgress_SecondPullStartsBarFromZero(t *testing.T) {
	p := NewPullProgress().Show("image-a")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pulling fs layer"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 100, Total: 100})
	p, cmd := p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Pull complete"})
	p = runFrames(t, p, cmd)
	require.Contains(t, p.bar.View(), "100%")
	p = p.Hide()

	p = p.Show("image-b")
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Pulling fs layer"})
	p, cmd = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 10, Total: 100})
	// The first frames of the new pull must not animate down from 100%.
	p, _ = p.Update(cmd())
	assert.NotContains(t, p.bar.View(), "100%")
	assert.Contains(t, p.bar.View(), " 0%")
}

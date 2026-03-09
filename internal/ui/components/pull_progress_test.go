package components

import (
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
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

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Downloading", Current: 50, Total: 100})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 25, Total: 100})

	pct := p.aggregatePercent()
	assert.InDelta(t, 0.375, pct, 0.001)
}

func TestPullProgress_ViewShowsLayerCounts(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

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

func TestPullProgress_AlreadyExistsCountsAsDone(t *testing.T) {
	p := NewPullProgress()
	p = p.Show("image")

	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "aaa", Status: "Already exists"})
	p, _ = p.SetProgress(output.ProgressEvent{LayerID: "bbb", Status: "Downloading", Current: 50, Total: 100})

	view := p.View()
	assert.Contains(t, view, "1/2")
	assert.Contains(t, view, "layers")
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

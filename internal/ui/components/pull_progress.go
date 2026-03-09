package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

type layerState struct {
	status   string
	current  int64
	total    int64
	maxTotal int64
	complete bool
}

type PullProgress struct {
	bar     progress.Model
	layers  map[string]*layerState
	visible bool
}

func NewPullProgress() PullProgress {
	bar := progress.New(
		progress.WithGradient(styles.NimboDarkColor, styles.NimboMidColor),
		progress.WithWidth(30),
		progress.WithFillCharacters('━', '─'),
	)
	bar.EmptyColor = "#3A3A3A"
	return PullProgress{bar: bar}
}

func (p PullProgress) Show(imageName string) PullProgress {
	p.layers = make(map[string]*layerState)
	p.visible = true
	return p
}

func (p PullProgress) Hide() PullProgress {
	p.visible = false
	p.layers = nil
	return p
}

func (p PullProgress) SetProgress(e output.ProgressEvent) (PullProgress, tea.Cmd) {
	if e.LayerID == "" {
		return p, nil
	}

	layer, ok := p.layers[e.LayerID]
	if !ok {
		layer = &layerState{}
		p.layers[e.LayerID] = layer
	}
	layer.status = e.Status
	layer.current = e.Current
	layer.total = e.Total
	if e.Total > layer.maxTotal {
		layer.maxTotal = e.Total
	}
	if e.Status == "Pull complete" || e.Status == "Already exists" {
		layer.complete = true
	}

	pct := p.aggregatePercent()
	cmd := p.bar.SetPercent(pct)
	return p, cmd
}

func (p PullProgress) aggregatePercent() float64 {
	var totalBytes, currentBytes int64
	for _, l := range p.layers {
		if l.maxTotal > 0 {
			totalBytes += l.maxTotal
			if l.complete {
				currentBytes += l.maxTotal
			} else {
				currentBytes += l.current
			}
		}
	}
	if totalBytes == 0 {
		return 0
	}
	return float64(currentBytes) / float64(totalBytes)
}

func (p PullProgress) Update(msg tea.Msg) (PullProgress, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	model, cmd := p.bar.Update(msg)
	p.bar = model.(progress.Model)
	return p, cmd
}

func (p PullProgress) Visible() bool {
	return p.visible
}

func (p PullProgress) View() string {
	if !p.visible {
		return ""
	}

	total, done := p.layerCounts()
	if total == 0 {
		return ""
	}

	phase := p.dominantPhase()
	// Fixed-width: "Downloading" is the longest phase (11 chars), counts padded to "XX/XX"
	label := fmt.Sprintf("%-11s %2d/%-2d layers", phase, done, total)
	return fmt.Sprintf("  %s  %s",
		styles.Secondary.Render(label),
		p.bar.View(),
	)
}

func (p PullProgress) layerCounts() (total, done int) {
	total = len(p.layers)
	for _, l := range p.layers {
		if l.complete {
			done++
		}
	}
	return total, done
}

func (p PullProgress) dominantPhase() string {
	downloading, extracting := 0, 0
	for _, l := range p.layers {
		switch l.status {
		case "Downloading":
			downloading++
		case "Extracting":
			extracting++
		}
	}
	if extracting > downloading {
		return "Extracting"
	}
	if downloading > 0 {
		return "Downloading"
	}
	return "Pulling"
}

package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

// Docker pull statuses the bar reacts to. The overlay2 and containerd image
// stores share these strings but differ in progressDetail; see testdata/.
const (
	statusPullingFsLayer   = "Pulling fs layer"
	statusWaiting          = "Waiting"
	statusDownloading      = "Downloading"
	statusDownloadComplete = "Download complete"
	statusExtracting       = "Extracting"
	statusPullComplete     = "Pull complete"
	statusAlreadyExists    = "Already exists"
	statusRetryingPrefix   = "Retrying in "
	statusHeaderPrefix     = "Pulling from "
)

// Share of a layer's weight per phase. containerd reports no extraction
// progress, so its share only fills on "Pull complete".
const (
	downloadWeight = 0.8
	extractWeight  = 1 - downloadWeight
)

// epsilon absorbs float noise from summing layers in map order.
const epsilon = 1e-9

// layerState tracks one blob. Byte counters only grow: Docker sends
// status-only events without counts and restarts counts for extraction.
type layerState struct {
	status string
	// counted blobs appear in the bar and the layer counter. containerd's
	// config/index blobs are never announced as layers and stay uncounted.
	counted      bool
	unannounced  bool // counted before Docker announced any layer
	cached       bool // "Already exists": no work in this pull
	total        int64
	downloaded   int64
	extracted    int64
	downloadDone bool
	complete     bool
}

type PullProgress struct {
	bar    progress.Model
	layers map[string]*layerState
	// announced is set once Docker announces any layer. Until then every blob
	// that reports bytes is counted, for runtimes that never announce layers.
	announced bool
	// estimate is the last raw progress estimate; percent is what the bar
	// shows. See advance.
	estimate float64
	percent  float64
	visible  bool
}

func newPullBar() progress.Model {
	bar := progress.New(
		progress.WithGradient(styles.NimboDarkColor, styles.NimboMidColor),
		progress.WithWidth(30),
		progress.WithFillCharacters('━', '─'),
	)
	bar.EmptyColor = "#3A3A3A"
	return bar
}

func NewPullProgress() PullProgress {
	return PullProgress{bar: newPullBar()}
}

// Show starts a new pull. The bar is rebuilt so it does not animate down from
// the previous pull's value, and stale animation frames are dropped.
func (p PullProgress) Show(imageName string) PullProgress {
	p.bar = newPullBar()
	p.layers = make(map[string]*layerState)
	p.announced = false
	p.estimate = 0
	p.percent = 0
	p.visible = true
	return p
}

func (p PullProgress) Hide() PullProgress {
	p.visible = false
	p.layers = nil
	return p
}

func (p PullProgress) SetProgress(e output.ProgressEvent) (PullProgress, tea.Cmd) {
	// Header ("Pulling from", id is the tag) and trailer lines are not blobs.
	if e.LayerID == "" || strings.HasPrefix(e.Status, statusHeaderPrefix) {
		return p, nil
	}

	layer, ok := p.layers[e.LayerID]
	if !ok {
		layer = &layerState{}
		p.layers[e.LayerID] = layer
	}
	layer.status = e.Status

	switch {
	case e.Status == statusPullingFsLayer, e.Status == statusWaiting:
		p.announce(layer)
	case e.Status == statusDownloading:
		layer.total = max(layer.total, e.Total)
		layer.downloaded = max(layer.downloaded, min(e.Current, layer.total))
		if !p.announced {
			layer.counted = true
			layer.unannounced = true
		}
	case e.Status == statusDownloadComplete:
		layer.downloadDone = true
		// Without announcements there is no "Pull complete" to wait for.
		layer.complete = layer.complete || layer.unannounced
	case strings.HasPrefix(e.Status, statusRetryingPrefix):
		layer.downloaded = 0
		layer.downloadDone = false
	case e.Status == statusExtracting:
		p.announce(layer)
		layer.downloadDone = true
		// overlay2 reports bytes; containerd reports seconds without a total.
		if e.Total > 0 {
			layer.total = max(layer.total, e.Total)
			layer.extracted = max(layer.extracted, min(e.Current, layer.total))
		}
	case e.Status == statusPullComplete:
		p.announce(layer)
		layer.downloadDone = true
		layer.complete = true
	case e.Status == statusAlreadyExists:
		p.announce(layer)
		layer.cached = true
		layer.complete = true
	}

	prev := p.percent
	p.advance(p.aggregatePercent())
	if p.percent <= prev {
		return p, nil
	}
	return p, p.bar.SetPercent(p.percent)
}

func (p *PullProgress) announce(l *layerState) {
	l.counted = true
	l.unannounced = false
	p.announced = true
}

// advance moves the shown percentage towards the new estimate without ever
// going backward or freezing. When a newly learned layer size drops the
// estimate below what is shown, later gains close the remaining distance
// proportionally, so the bar keeps moving and reaches 100% with the pull.
func (p *PullProgress) advance(estimate float64) {
	if gain := estimate - p.estimate; gain > epsilon && p.estimate < 1 {
		p.percent += (1 - p.percent) * gain / (1 - p.estimate)
	}
	p.percent = min(1, max(p.percent, estimate))
	p.estimate = estimate
}

// aggregatePercent estimates progress over the counted, non-cached layers.
// Docker reports most sizes only once a download starts, so unknown sizes are
// guessed: a layer that finished without a size was tiny (weight 0), a pending
// one may be large (largest known size).
func (p PullProgress) aggregatePercent() float64 {
	var largest int64
	for _, l := range p.pending() {
		largest = max(largest, l.total)
	}

	var totalWeight, doneWeight float64
	for _, l := range p.pending() {
		if largest == 0 { // no size known yet: only finished layers count
			totalWeight++
			if l.complete {
				doneWeight++
			}
			continue
		}
		weight := float64(l.total)
		switch {
		case l.total > 0:
		case l.downloadDone:
			weight = 0
		default:
			weight = float64(largest)
		}
		totalWeight += weight
		doneWeight += weight * (downloadWeight*l.downloadFraction() + extractWeight*l.extractFraction())
	}
	if totalWeight == 0 {
		return 0
	}
	return doneWeight / totalWeight
}

// pending returns the layers this pull has to fetch.
func (p PullProgress) pending() []*layerState {
	var out []*layerState
	for _, l := range p.layers {
		if l.counted && !l.cached {
			out = append(out, l)
		}
	}
	return out
}

func (l *layerState) downloadFraction() float64 {
	switch {
	case l.downloadDone:
		return 1
	case l.total > 0:
		return float64(l.downloaded) / float64(l.total)
	default:
		return 0
	}
}

func (l *layerState) extractFraction() float64 {
	switch {
	case l.complete:
		return 1
	case l.total > 0:
		return float64(l.extracted) / float64(l.total)
	default:
		return 0
	}
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

	// Fixed-width: "Downloading" is the longest phase (11 chars), counts padded to "XX/XX"
	label := fmt.Sprintf("%-11s %2d/%-2d layers", p.phase(), done, total)
	return fmt.Sprintf("  %s  %s",
		label,
		p.bar.View(),
	)
}

// layerCounts counts the same layers as the bar, so both agree.
func (p PullProgress) layerCounts() (total, done int) {
	for _, l := range p.pending() {
		total++
		if l.complete {
			done++
		}
	}
	return total, done
}

// phase names the furthest stage all layers have reached, so the label moves
// forward only: Pulling, Downloading, Extracting, Complete.
func (p PullProgress) phase() string {
	started, downloading, extracting := false, false, false
	for _, l := range p.pending() {
		started = started || l.downloaded > 0 || l.downloadDone
		if !l.downloadDone {
			downloading = true
		} else if !l.complete {
			extracting = true
		}
	}
	switch {
	case !started:
		return "Pulling"
	case downloading:
		return "Downloading"
	case extracting:
		return "Extracting"
	default:
		return "Complete"
	}
}

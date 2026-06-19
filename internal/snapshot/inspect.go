package snapshot

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// ComputeInspect opens a local .snapshot archive (a ZIP) and tallies its byte
// usage per service, combining control-plane state (api_states/) and data-asset
// payloads (assets/). It needs no running emulator, platform call, or auth.
// Services are sorted largest-first by uncompressed size.
func ComputeInspect(path string) (output.SnapshotInspectedEvent, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return output.SnapshotInspectedEvent{}, err
	}
	defer func() { _ = r.Close() }()

	type agg struct{ unc, comp int64 }
	totals := map[string]*agg{}
	var order []string
	ev := output.SnapshotInspectedEvent{Path: path}

	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "/") {
			continue // directory entry, no payload
		}
		unc := int64(f.UncompressedSize64)
		comp := int64(f.CompressedSize64)
		ev.TotalUncompressed += unc
		ev.TotalCompressed += comp

		svc := serviceOf(f.Name)
		a := totals[svc]
		if a == nil {
			a = &agg{}
			totals[svc] = a
			order = append(order, svc)
		}
		a.unc += unc
		a.comp += comp
	}

	ev.Services = make([]output.SnapshotServiceSize, 0, len(order))
	for _, svc := range order {
		a := totals[svc]
		ev.Services = append(ev.Services, output.SnapshotServiceSize{
			Service:      svc,
			Uncompressed: a.unc,
			Compressed:   a.comp,
		})
	}
	sort.SliceStable(ev.Services, func(i, j int) bool {
		if ev.Services[i].Uncompressed != ev.Services[j].Uncompressed {
			return ev.Services[i].Uncompressed > ev.Services[j].Uncompressed
		}
		return ev.Services[i].Service < ev.Services[j].Service
	})
	return ev, nil
}

// Inspect computes a local snapshot's size breakdown and emits it as a
// SnapshotInspectedEvent. On a read/parse error it emits a friendly ErrorEvent
// and returns a silent error.
func Inspect(path string, sink output.Sink) error {
	ev, err := ComputeInspect(path)
	if err != nil {
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("Could not read snapshot %q", path),
			Summary: "The file is not a valid snapshot archive.",
			Actions: []output.ErrorAction{
				{Label: "Inspect a saved snapshot:", Value: "lstk snapshot inspect ./my-snapshot.snapshot"},
			},
		})
		return output.NewSilentError(fmt.Errorf("inspect snapshot %q: %w", path, err))
	}
	sink.Emit(output.DeferredEvent{Inner: ev})
	return nil
}

// InspectJSON writes a local snapshot's size breakdown to w as JSON. This is a
// machine-output mode used by `snapshot inspect --json`; sizes are raw bytes so
// callers (scripts, agents) can compute their own percentages.
func InspectJSON(path string, w io.Writer) error {
	ev, err := ComputeInspect(path)
	if err != nil {
		return fmt.Errorf("inspect snapshot %q: %w", path, err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(ev)
}

// serviceOf maps an archive entry path to the AWS service it belongs to. The two
// known layouts place the service at different depths:
//
//	api_states/<account>/<service>/[<region>/]...  -> service
//	assets/<service>/...                           -> service
//
// Services are aggregated across accounts and regions. Anything else (root
// files, unrecognized layouts) is bucketed under "(other)".
func serviceOf(name string) string {
	name = strings.TrimPrefix(name, "./")
	parts := strings.Split(name, "/")
	switch {
	case len(parts) >= 3 && parts[0] == "api_states" && parts[2] != "":
		return parts[2]
	case len(parts) >= 2 && parts[0] == "assets" && parts[1] != "":
		return parts[1]
	default:
		return "(other)"
	}
}

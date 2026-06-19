package snapshot_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/localstack/lstk/internal/snapshot"
)

// writeSnapshot builds a .snapshot ZIP whose entries are stored (uncompressed)
// so each entry's compressed and uncompressed sizes equal its byte length.
func writeSnapshot(t *testing.T, entries map[string]int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.snapshot")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatalf("create header %q: %v", name, err)
		}
		if _, err := w.Write(bytes.Repeat([]byte("x"), entries[name])); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func TestComputeInspect(t *testing.T) {
	t.Parallel()

	path := writeSnapshot(t, map[string]int{
		"api_states/": 0, // directory entry, must be skipped
		// api_states/<account>/<service>/<region>/...: service is the 3rd segment,
		// aggregated across accounts and regions.
		"api_states/000000000000/s3/us-east-1/store.state.avro":       100,
		"api_states/949334387222/s3/us-east-1/store.state.avro":       30, // 2nd account, same service
		"api_states/000000000000/dynamodb/us-east-1/store.state.avro": 50,
		// assets/<service>/...: service is the 2nd segment.
		"assets/ecr/layer1":                         1000,
		"assets/rds/dump.sql":                       400,
		"assets/dynamodb/000000000000_us-east-1.db": 200,
		"manifest.json":                             10, // root-level file -> (other)
	})

	ev, err := snapshot.ComputeInspect(path)
	if err != nil {
		t.Fatalf("ComputeInspect: %v", err)
	}

	if got, want := ev.TotalUncompressed, int64(1790); got != want {
		t.Fatalf("TotalUncompressed = %d, want %d", got, want)
	}
	if got, want := ev.TotalCompressed, int64(1790); got != want {
		t.Fatalf("TotalCompressed = %d, want %d (stored entries)", got, want)
	}

	// One flat row per service, combining control-plane state and data assets,
	// aggregated across accounts/regions, sorted largest-first:
	//   ecr 1000 (data) > rds 400 (data) > dynamodb 250 (50 state + 200 data) >
	//   s3 130 (100+30 state, two accounts) > (other) 10 (manifest.json).
	want := []struct {
		service string
		size    int64
	}{
		{"ecr", 1000},
		{"rds", 400},
		{"dynamodb", 250},
		{"s3", 130},
		{"(other)", 10},
	}
	if len(ev.Services) != len(want) {
		t.Fatalf("services = %+v, want %d entries", ev.Services, len(want))
	}
	for i, w := range want {
		if ev.Services[i].Service != w.service || ev.Services[i].Uncompressed != w.size {
			t.Fatalf("service[%d] = %+v, want %s %d", i, ev.Services[i], w.service, w.size)
		}
	}
	for _, s := range ev.Services {
		if s.Service == "000000000000" || s.Service == "949334387222" {
			t.Fatalf("service %q is an account id; services must aggregate across accounts", s.Service)
		}
	}
}

func TestComputeInspectInvalidFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-a-zip.snapshot")
	if err := os.WriteFile(path, []byte("definitely not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.ComputeInspect(path); err == nil {
		t.Fatal("expected error for a non-zip file, got nil")
	}
}

func TestInspectJSON(t *testing.T) {
	t.Parallel()

	path := writeSnapshot(t, map[string]int{
		"assets/ecr/layer1": 1000,
		"api_states/000000000000/s3/us-east-1/store.state.avro": 100,
	})

	var buf bytes.Buffer
	if err := snapshot.InspectJSON(path, &buf); err != nil {
		t.Fatalf("InspectJSON: %v", err)
	}

	var got struct {
		TotalUncompressed int64 `json:"total_uncompressed_bytes"`
		Services          []struct {
			Service      string `json:"service"`
			Uncompressed int64  `json:"uncompressed_bytes"`
		} `json:"services"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, buf.String())
	}
	if got.TotalUncompressed != 1100 {
		t.Fatalf("total = %d, want 1100", got.TotalUncompressed)
	}
	if len(got.Services) == 0 || got.Services[0].Service != "ecr" || got.Services[0].Uncompressed != 1000 {
		t.Fatalf("services = %+v, want ecr (1000) first", got.Services)
	}
}

func TestParseInspectable(t *testing.T) {
	t.Parallel()

	t.Run("rejects pod refs pointing at show", func(t *testing.T) {
		t.Parallel()
		if _, err := snapshot.ParseInspectable("pod:my-baseline", ""); err == nil {
			t.Fatal("expected error for pod: ref")
		}
	})

	t.Run("rejects remote schemes", func(t *testing.T) {
		t.Parallel()
		if _, err := snapshot.ParseInspectable("s3://bucket/key", ""); err == nil {
			t.Fatal("expected error for s3:// ref")
		}
	})

	t.Run("accepts an existing local file", func(t *testing.T) {
		t.Parallel()
		path := writeSnapshot(t, map[string]int{"assets/s3/obj1": 1})
		dest, err := snapshot.ParseInspectable(path, "")
		if err != nil {
			t.Fatalf("ParseInspectable: %v", err)
		}
		if dest.Kind != snapshot.KindLocal || dest.Value != path {
			t.Fatalf("dest = %+v, want KindLocal %q", dest, path)
		}
	})

	t.Run("errors on a missing local file", func(t *testing.T) {
		t.Parallel()
		if _, err := snapshot.ParseInspectable(filepath.Join(t.TempDir(), "nope.snapshot"), ""); err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

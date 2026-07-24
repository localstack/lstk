package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// makeReleaseArchive builds an archive in the format updateBinary expects for
// the current GOOS (zip on windows, tar.gz elsewhere) containing a single
// binary entry at the archive root.
func makeReleaseArchive(t *testing.T, binaryContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if goruntime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("lstk.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(binaryContent)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "lstk",
		Mode:     0o755,
		Size:     int64(len(binaryContent)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binaryContent)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeExecutable writes a stand-in for the running binary into its own temp
// dir and returns its path.
func fakeExecutable(t *testing.T, content string) string {
	t.Helper()
	name := "lstk"
	if goruntime.GOOS == "windows" {
		name = "lstk.exe"
	}
	exe := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// releaseServer serves GET /<tag>/<name> for the given assets, mimicking the
// GitHub release download URL layout.
func releaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/"+tag+"/")
		body, ok := assets[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func assertNoTempLeftovers(t *testing.T, exe string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(exe), "lstk-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestBinaryUpdaterVerifiesChecksum(t *testing.T) {
	t.Parallel()

	const tag = "v0.9.9"
	assetName := buildAssetName(normalizeVersion(tag), goruntime.GOOS, goruntime.GOARCH)
	archive := makeReleaseArchive(t, "new-binary-content")
	archiveSum := sha256.Sum256(archive)
	validManifest := fmt.Sprintf("%s  %s\n", hex.EncodeToString(archiveSum[:]), assetName)

	tests := []struct {
		name        string
		assets      map[string][]byte
		wantErr     string
		wantContent string
	}{
		{
			name: "happy path replaces binary",
			assets: map[string][]byte{
				"checksums.txt": []byte(validManifest),
				assetName:       archive,
			},
			wantContent: "new-binary-content",
		},
		{
			name: "checksum mismatch aborts before replace",
			assets: map[string][]byte{
				"checksums.txt": fmt.Appendf(nil, "%s  %s\n", strings.Repeat("0", 64), assetName),
				assetName:       archive,
			},
			wantErr:     "checksum mismatch",
			wantContent: "old-binary-content",
		},
		{
			name: "missing checksums.txt refuses install",
			assets: map[string][]byte{
				assetName: archive,
			},
			wantErr:     "refusing to install an unverifiable binary",
			wantContent: "old-binary-content",
		},
		{
			name: "manifest missing asset entry refuses install",
			assets: map[string][]byte{
				"checksums.txt": fmt.Appendf(nil, "%s  some-other-asset.tar.gz\n", strings.Repeat("0", 64)),
				assetName:       archive,
			},
			wantErr:     "no entry for " + assetName,
			wantContent: "old-binary-content",
		},
		{
			name: "malformed manifest refuses install",
			assets: map[string][]byte{
				"checksums.txt": []byte("not a checksum manifest\n"),
				assetName:       archive,
			},
			wantErr:     "malformed checksum manifest",
			wantContent: "old-binary-content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := releaseServer(t, tag, tt.assets)
			exe := fakeExecutable(t, "old-binary-content")

			u := &binaryUpdater{
				downloadBase: server.URL,
				resolveExe:   func() (string, error) { return exe, nil },
			}
			err := u.update(context.Background(), tag, "")

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("update() error = %v, want containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("update() unexpected error: %v", err)
			}

			got, readErr := os.ReadFile(exe)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tt.wantContent {
				t.Errorf("binary content = %q, want %q", got, tt.wantContent)
			}
			assertNoTempLeftovers(t, exe)
		})
	}
}

func TestNewBinaryUpdaterDefaults(t *testing.T) {
	t.Parallel()
	u := newBinaryUpdater()
	if u.downloadBase != "https://github.com/localstack/lstk/releases/download" {
		t.Errorf("downloadBase = %q", u.downloadBase)
	}
	exe, err := u.resolveExe()
	if err != nil {
		t.Fatalf("resolveExe() error: %v", err)
	}
	if exe == "" {
		t.Error("resolveExe() returned empty path")
	}
}

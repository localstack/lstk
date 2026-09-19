package container

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"go.uber.org/mock/gomock"
)

// FuzzStartContainers_ShortContainerID targets the containerID[:12] slice at
// start.go:330,333. If the runtime returns an ID shorter than 12 bytes, the
// program panics with an index-out-of-bounds error.
func FuzzStartContainers_ShortContainerID(f *testing.F) {
	f.Add("")
	f.Add("a")
	f.Add("short")
	f.Add("exactly12ch")
	f.Add("abcdef123456")                                                     // exactly 12
	f.Add("abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890") // 64 (normal Docker ID)
	f.Add("abc")
	f.Add("\x00\x00\x00")

	f.Fuzz(func(t *testing.T, containerID string) {
		ctrl := gomock.NewController(t)
		mockRT := runtime.NewMockRuntime(ctrl)

		// Start returns the fuzzed containerID
		mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return(containerID, nil).AnyTimes()

		// awaitStartup needs IsRunning + a health endpoint
		// Spin up a local HTTP server that returns 200 immediately
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skip("cannot bind listener")
		}
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})}
		go srv.Serve(listener)
		defer srv.Close()

		mockRT.EXPECT().IsRunning(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		mockRT.EXPECT().Logs(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

		containers := []runtime.ContainerConfig{{
			Image:         "localstack/localstack-pro:stable",
			Name:          "localstack-aws",
			EmulatorType:  "aws",
			Port:          port,
			ContainerPort: "4566/tcp",
			HealthPath:    "/_localstack/health",
			Tag:           "stable",
			ProductName:   "localstack-pro",
		}}

		sink := output.NewPlainSink(io.Discard)

		// This should never panic — but containerID[:12] will if len < 12
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("PANIC with containerID=%q (len=%d): %v", containerID, len(containerID), r)
			}
		}()

		_ = startContainers(context.Background(), mockRT, sink, nil, containers, map[string]bool{})
	})
}

// TestStartContainers_EmptyContainerID is a direct regression test for the
// containerID[:12] panic. Not fuzzed — just proves the bug exists.
func TestStartContainers_EmptyContainerID_Panics(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return("", nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	mockRT.EXPECT().Logs(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	// Local health server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(listener)
	defer srv.Close()

	containers := []runtime.ContainerConfig{{
		Image:         "localstack/localstack-pro:stable",
		Name:          "localstack-aws",
		EmulatorType:  "aws",
		Port:          port,
		ContainerPort: "4566/tcp",
		HealthPath:    "/_localstack/health",
		Tag:           "stable",
		ProductName:   "localstack-pro",
	}}

	sink := output.NewPlainSink(io.Discard)

	defer func() {
		if r := recover(); r != nil {
			// Expected: this proves the bug
			fmt.Printf("CONFIRMED BUG: containerID[:12] panics with empty ID: %v\n", r)
		} else {
			t.Error("Expected panic from containerID[:12] with empty string, but got none")
		}
	}()

	_ = startContainers(context.Background(), mockRT, sink, nil, containers, map[string]bool{})
}

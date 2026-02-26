package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestResolveRelayAdminSocketPath(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("dir", "", "state dir")
	if err := cmd.Flags().Set("dir", "/tmp/aqua-test"); err != nil {
		t.Fatalf("set dir flag: %v", err)
	}

	gotDefault, err := resolveRelayAdminSocketPath(cmd, "")
	if err != nil {
		t.Fatalf("resolveRelayAdminSocketPath(default) error = %v", err)
	}
	wantDefault := filepath.Join("/tmp/aqua-test", relayAdminSocketBaseName)
	if gotDefault != wantDefault {
		t.Fatalf("default socket path = %q, want %q", gotDefault, wantDefault)
	}

	gotExplicit, err := resolveRelayAdminSocketPath(cmd, "/tmp/custom.sock")
	if err != nil {
		t.Fatalf("resolveRelayAdminSocketPath(explicit) error = %v", err)
	}
	if gotExplicit != "/tmp/custom.sock" {
		t.Fatalf("explicit socket path = %q, want %q", gotExplicit, "/tmp/custom.sock")
	}
}

func TestResolveRelayStatusPeaksPath(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("dir", "", "state dir")
	if err := cmd.Flags().Set("dir", "/tmp/aqua-test"); err != nil {
		t.Fatalf("set dir flag: %v", err)
	}

	got, err := resolveRelayStatusPeaksPath(cmd)
	if err != nil {
		t.Fatalf("resolveRelayStatusPeaksPath() error = %v", err)
	}
	want := filepath.Join("/tmp/aqua-test", relayStatusPeaksFileBaseName)
	if got != want {
		t.Fatalf("status peaks path = %q, want %q", got, want)
	}
}

func TestRelayPeersCommandJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sock := filepath.Join(dir, relayAdminSocketBaseName)
	now := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(20 * time.Minute)
	expiresInSec := int64(20 * time.Minute / time.Second)

	shutdown := startRelayPeersUnixServer(t, sock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peers" {
			http.NotFound(w, r)
			return
		}
		_ = writeJSON(w, relayPeersAPIView{
			Now: now,
			Peers: []relayPeerView{
				{
					PeerID:    "12D3KooWExamplePeerA",
					ExpiresAt: &expiresAt,
				},
			},
		})
	}))
	defer shutdown()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "relay", "peers", "--json")
	if err != nil {
		t.Fatalf("relay peers --json error = %v, stderr=%s", err, stderr)
	}

	var view relayPeersView
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode relay peers output json error = %v, stdout=%s", err, stdout)
	}
	if len(view.Peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(view.Peers))
	}
	if view.Peers[0].ExpiresInSec == nil || *view.Peers[0].ExpiresInSec != expiresInSec {
		t.Fatalf("expires_in_sec = %v, want %d", view.Peers[0].ExpiresInSec, expiresInSec)
	}
	if view.Peers[0].ExpiresIn != "20m" {
		t.Fatalf("expires_in = %q, want %q", view.Peers[0].ExpiresIn, "20m")
	}
}

func TestRelayPeersCommandText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sock := filepath.Join(dir, relayAdminSocketBaseName)
	now := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)

	shutdown := startRelayPeersUnixServer(t, sock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peers" {
			http.NotFound(w, r)
			return
		}
		_ = writeJSON(w, relayPeersAPIView{
			Now: now,
			Peers: []relayPeerView{
				{
					PeerID: "12D3KooWExamplePeerB",
				},
			},
		})
	}))
	defer shutdown()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "relay", "peers")
	if err != nil {
		t.Fatalf("relay peers text error = %v, stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "peer_id: 12D3KooWExamplePeerB") {
		t.Fatalf("expected peer_id in output, got: %s", stdout)
	}
	if strings.Contains(stdout, "lease_status:") {
		t.Fatalf("did not expect lease_status in output, got: %s", stdout)
	}
	if strings.Contains(stdout, "status:") {
		t.Fatalf("did not expect status in output, got: %s", stdout)
	}
}

func TestRelayPeersCommandErrorStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sock := filepath.Join(dir, relayAdminSocketBaseName)
	shutdown := startRelayPeersUnixServer(t, sock, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer shutdown()

	_, stderr, err := executeCLI(t, "--dir", dir, "relay", "peers")
	if err == nil {
		t.Fatalf("expected relay peers command to fail on non-200 status")
	}
	if !strings.Contains(fmt.Sprint(err), "returned 502 Bad Gateway") && !strings.Contains(stderr, "502 Bad Gateway") {
		t.Fatalf("expected 502 in error output, got err=%v stderr=%s", err, stderr)
	}
}

func startRelayPeersUnixServer(t *testing.T, socketPath string, handler http.Handler) func() {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatalf("create socket parent dir: %v", err)
	}
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}
}

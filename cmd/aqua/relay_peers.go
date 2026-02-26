package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newRelayPeersCmd() *cobra.Command {
	var adminSock string
	var timeout time.Duration
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "peers",
		Short: "List currently connected relay peers with active reservations",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedAdminSock, err := resolveRelayAdminSocketPath(cmd, adminSock)
			if err != nil {
				return err
			}
			view, err := fetchRelayPeersFromUnixSocket(cmd.Context(), resolvedAdminSock, timeout)
			if err != nil {
				return err
			}

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			writeRelayPeersText(cmd.OutOrStdout(), view)
			return nil
		},
	}

	cmd.Flags().StringVar(&adminSock, "admin-sock", "", "Relay admin unix socket path (default: <AQUA_DIR>/relay-admin.sock)")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Second, "HTTP request timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func fetchRelayPeersFromUnixSocket(ctx context.Context, socketPath string, timeout time.Duration) (relayPeersView, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return relayPeersView{}, fmt.Errorf("empty relay admin socket path")
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	defer transport.CloseIdleConnections()

	requestURL := "http://relay-admin/peers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return relayPeersView{}, fmt.Errorf("build relay peers request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return relayPeersView{}, fmt.Errorf("request relay peers from unix socket %s: %w", socketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return relayPeersView{}, fmt.Errorf("relay peers endpoint %s returned %s: %s", requestURL, resp.Status, msg)
	}

	var apiView relayPeersAPIView
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&apiView); err != nil {
		return relayPeersView{}, fmt.Errorf("decode relay peers response: %w", err)
	}
	return relayPeersCommandViewFromAPI(apiView), nil
}

func writeRelayPeersText(w io.Writer, view relayPeersView) {
	peers := append([]relayPeerCommandView(nil), view.Peers...)
	sort.SliceStable(peers, func(i, j int) bool {
		leftHasExpires := peers[i].ExpiresAt != nil && !peers[i].ExpiresAt.IsZero()
		rightHasExpires := peers[j].ExpiresAt != nil && !peers[j].ExpiresAt.IsZero()
		if leftHasExpires && rightHasExpires {
			left := canonicalRelayStatusTime(*peers[i].ExpiresAt)
			right := canonicalRelayStatusTime(*peers[j].ExpiresAt)
			if !left.Equal(right) {
				return left.Before(right)
			}
			return peers[i].PeerID < peers[j].PeerID
		}
		if leftHasExpires != rightHasExpires {
			return leftHasExpires
		}
		return peers[i].PeerID < peers[j].PeerID
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "peer_id\texpires_at\texpires_in")
	for _, item := range peers {
		expiresAt := "-"
		if item.ExpiresAt != nil && !item.ExpiresAt.IsZero() {
			expiresAt = canonicalRelayStatusTime(*item.ExpiresAt).Format(time.RFC3339)
		}
		expiresIn := strings.TrimSpace(item.ExpiresIn)
		if expiresIn == "" {
			expiresIn = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", item.PeerID, expiresAt, expiresIn)
	}
	_ = tw.Flush()
}

type relayPeersView struct {
	Now   time.Time              `json:"now"`
	Peers []relayPeerCommandView `json:"peers"`
}

type relayPeerCommandView struct {
	PeerID       string     `json:"peer_id"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	ExpiresInSec *int64     `json:"expires_in_sec,omitempty"`
	ExpiresIn    string     `json:"expires_in,omitempty"`
}

func relayPeersCommandViewFromAPI(view relayPeersAPIView) relayPeersView {
	now := canonicalRelayStatusTime(view.Now)
	if now.IsZero() {
		now = canonicalRelayStatusTime(time.Now().UTC())
	}

	peers := make([]relayPeerCommandView, 0, len(view.Peers))
	for _, item := range view.Peers {
		cmdView := relayPeerCommandView{
			PeerID: item.PeerID,
		}
		if item.ExpiresAt != nil {
			expiresAt := canonicalRelayStatusTime(*item.ExpiresAt)
			expiresIn := expiresAt.Sub(now)
			expiresInSec := int64(expiresIn / time.Second)
			cmdView.ExpiresAt = &expiresAt
			cmdView.ExpiresInSec = &expiresInSec
			cmdView.ExpiresIn = formatRelayRelativeDuration(expiresIn)
		}
		peers = append(peers, cmdView)
	}
	sort.SliceStable(peers, func(i, j int) bool {
		return peers[i].PeerID < peers[j].PeerID
	})

	return relayPeersView{
		Now:   now,
		Peers: peers,
	}
}

func formatRelayRelativeDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	d = d.Round(time.Second)
	if d < time.Second {
		return sign + "0s"
	}
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%s%dh%dm", sign, hours, minutes)
		}
		return fmt.Sprintf("%s%dh", sign, hours)
	}
	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%s%dm%ds", sign, minutes, seconds)
		}
		return fmt.Sprintf("%s%dm", sign, minutes)
	}
	return fmt.Sprintf("%s%ds", sign, seconds)
}

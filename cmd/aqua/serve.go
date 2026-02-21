package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var listenAddrs []string
	var relayAddrs []string
	var relayMode string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Aqua libp2p node and handle incoming RPC streams",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			svc := serviceFromCmd(cmd)
			identity, _, err := svc.EnsureIdentity(runCtx, time.Now().UTC())
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
			node, err := aqua.NewNode(runCtx, svc, aqua.NodeOptions{
				ListenAddrs: listenAddrs,
				RelayAddrs:  relayAddrs,
				RelayMode:   relayMode,
				Logger:      logger,
				OnDataPush: func(event aqua.DataPushEvent) {
					printDataPushEvent(cmd, event, outputJSON)
				},
				OnRelayEvent: func(event aqua.RelayEvent) {
					printRelayEvent(cmd, event, outputJSON)
				},
			})
			if err != nil {
				return err
			}
			defer node.Close()

			if outputJSON {
				_ = writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":      "ready",
					"peer_id":     node.PeerID(),
					"node_uuid":   identity.NodeUUID,
					"addresses":   node.AddrStrings(),
					"relay_addrs": relayAddrs,
					"relay_mode":  relayMode,
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nnode_uuid: %s\npeer_id: %s\nrelay_mode: %s\n", identity.NodeUUID, node.PeerID(), relayMode)
				for _, relayAddr := range relayAddrs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "relay: %s\n", relayAddr)
				}
				for _, addr := range node.AddrStrings() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for incoming streams... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Listen multiaddr (repeatable), default tries /ip4/0.0.0.0/udp/6371/quic-v1, /ip4/0.0.0.0/tcp/6371 and /ip4/0.0.0.0/tcp/6372/ws, then falls back to random ports")
	cmd.Flags().StringArrayVar(&relayAddrs, "relay", nil, "Relay multiaddr (repeatable, must end with /p2p/<relay_peer_id>)")
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status/events as JSON")
	return cmd
}

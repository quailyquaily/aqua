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
				Logger:      logger,
				OnDataPush: func(event aqua.DataPushEvent) {
					printDataPushEvent(cmd, event, outputJSON)
				},
			})
			if err != nil {
				return err
			}
			defer node.Close()

			if outputJSON {
				_ = writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":    "ready",
					"peer_id":   node.PeerID(),
					"node_uuid": identity.NodeUUID,
					"addresses": node.AddrStrings(),
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nnode_uuid: %s\npeer_id: %s\n", identity.NodeUUID, node.PeerID())
				for _, addr := range node.AddrStrings() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for incoming streams... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Listen multiaddr (repeatable), default tries /ip4/0.0.0.0/udp/6371/quic-v1 and /ip4/0.0.0.0/tcp/6371, then falls back to random ports")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status/events as JSON")
	return cmd
}

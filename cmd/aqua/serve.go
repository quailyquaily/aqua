package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

var serveDefaultListenAddrs = []string{
	"/ip4/0.0.0.0/udp/6371/quic-v1",
	"/ip4/0.0.0.0/tcp/6371",
	"/ip4/0.0.0.0/tcp/6372/ws",
}

func newServeCmd() *cobra.Command {
	var listenAddrs []string
	var relayAddrs []string
	var relayMode string
	var outputJSON bool
	var dryRun bool
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
			logger, err := loggerFromCmd(cmd)
			if err != nil {
				return err
			}
			resolvedRelayMode, err := normalizeServeRelayMode(relayMode)
			if err != nil {
				return err
			}
			resolvedListenAddrs := normalizeAddressList(listenAddrs)
			resolvedRelayAddrs := normalizeAddressList(relayAddrs)
			logger.Debug(
				"serve config",
				"listen_addrs", resolvedListenAddrs,
				"relay_addrs", resolvedRelayAddrs,
				"relay_mode", resolvedRelayMode,
				"dryrun", dryRun,
			)

			if dryRun {
				resolvedListenAddrs, resolvedAddresses, err := resolveServeDryRunAddresses(listenAddrs, relayAddrs, identity.PeerID)
				if err != nil {
					return err
				}
				if outputJSON {
					_ = writeJSON(cmd.OutOrStdout(), map[string]any{
						"status":       "dryrun",
						"dryrun":       true,
						"peer_id":      identity.PeerID,
						"node_uuid":    identity.NodeUUID,
						"listen_addrs": resolvedListenAddrs,
						"relay_addrs":  resolvedRelayAddrs,
						"relay_mode":   resolvedRelayMode,
						"addresses":    resolvedAddresses,
					})
					return nil
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: dryrun\ndryrun: true\nnode_uuid: %s\npeer_id: %s\nrelay_mode: %s\n", identity.NodeUUID, identity.PeerID, resolvedRelayMode)
				for _, listenAddr := range resolvedListenAddrs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "listen: %s\n", listenAddr)
				}
				for _, relayAddr := range resolvedRelayAddrs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "relay: %s\n", relayAddr)
				}
				for _, addr := range resolvedAddresses {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				return nil
			}

			node, err := aqua.NewNode(runCtx, svc, aqua.NodeOptions{
				ListenAddrs: listenAddrs,
				RelayAddrs:  relayAddrs,
				RelayMode:   resolvedRelayMode,
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
					"relay_addrs": resolvedRelayAddrs,
					"relay_mode":  resolvedRelayMode,
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nnode_uuid: %s\npeer_id: %s\nrelay_mode: %s\n", identity.NodeUUID, node.PeerID(), resolvedRelayMode)
				for _, relayAddr := range resolvedRelayAddrs {
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
	cmd.Flags().BoolVar(&dryRun, "dryrun", false, "Print derived addresses and exit without listening")
	return cmd
}

func normalizeServeRelayMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return aqua.RelayModeAuto, nil
	}
	switch mode {
	case aqua.RelayModeAuto, aqua.RelayModeOff, aqua.RelayModeRequired:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --relay-mode %q (supported: %s, %s, %s)", raw, aqua.RelayModeAuto, aqua.RelayModeOff, aqua.RelayModeRequired)
	}
}

func resolveServeDryRunAddresses(listenAddrs []string, relayAddrs []string, localPeerID string) ([]string, []string, error) {
	resolvedListenAddrs := normalizeAddressList(listenAddrs)
	if len(resolvedListenAddrs) == 0 {
		resolvedListenAddrs = append([]string(nil), serveDefaultListenAddrs...)
	}

	directBase := expandAdvertiseAddressesForListenAddrs(resolvedListenAddrs, resolvedListenAddrs)
	directAddrs, err := appendPeerIDToAddresses(directBase, localPeerID)
	if err != nil {
		return nil, nil, err
	}

	relayAdvertiseAddrs := []string{}
	if len(normalizeAddressList(relayAddrs)) > 0 {
		relayAdvertiseAddrs, err = buildRelayAdvertiseAddresses(relayAddrs, localPeerID)
		if err != nil {
			return nil, nil, err
		}
	}

	resolvedAddresses := normalizeAddressList(append(directAddrs, relayAdvertiseAddrs...))
	return resolvedListenAddrs, resolvedAddresses, nil
}

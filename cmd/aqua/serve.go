package main

import (
	"context"
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
	"/ip4/0.0.0.0/udp/6372/quic-v1",
	"/ip4/0.0.0.0/tcp/6372",
}

const envRelayProbe = "AQUA_RELAY_PROBE"

func newServeCmd() *cobra.Command {
	var listenAddrs []string
	var relayAddrs []string
	var relayMode string
	var webConsole bool
	var webConsoleAddr string
	var consoleWebDstDir string
	var webhookURL string
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
			resolvedWebhookURL, err := resolveServeWebhookURL(webhookURL)
			if err != nil {
				return err
			}
			relayProbeEnabled, err := resolveRelayProbeEnabledFromEnv()
			if err != nil {
				return err
			}
			resolvedListenAddrs := normalizeAddressList(listenAddrs)
			resolvedRelayAddrs := normalizeAddressList(relayAddrs)
			if !webConsole && strings.TrimSpace(consoleWebDstDir) != "" {
				return fmt.Errorf("--console-web-dst-dir requires --web-console")
			}
			logger.Debug(
				"serve config",
				"listen_addrs", resolvedListenAddrs,
				"relay_addrs", resolvedRelayAddrs,
				"relay_mode", resolvedRelayMode,
				"relay_probe", relayProbeEnabled,
				"web_console", webConsole,
				"web_console_addr", strings.TrimSpace(webConsoleAddr),
				"console_web_dst_dir", strings.TrimSpace(consoleWebDstDir),
				"webhook_url", resolvedWebhookURL,
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
						"relay_probe":  relayProbeEnabled,
						"addresses":    resolvedAddresses,
						"webhook_url":  resolvedWebhookURL,
					})
					return nil
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: dryrun\ndryrun: true\nnode_uuid: %s\npeer_id: %s\nrelay_mode: %s\nrelay_probe: %t\n", identity.NodeUUID, identity.PeerID, resolvedRelayMode, relayProbeEnabled)
				if resolvedWebhookURL != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "webhook: %s\n", resolvedWebhookURL)
				}
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

			var consoleServer *webConsoleServer
			webhookDispatcher, err := newDataPushWebhookDispatcher(runCtx, resolvedWebhookURL, logger)
			if err != nil {
				return err
			}
			defer webhookDispatcher.close()

			node, err := aqua.NewNode(runCtx, svc, aqua.NodeOptions{
				ListenAddrs:       listenAddrs,
				RelayAddrs:        relayAddrs,
				RelayMode:         resolvedRelayMode,
				RelayProbeEnabled: relayProbeEnabled,
				Logger:            logger,
				OnDataPush: func(event aqua.DataPushEvent) {
					printDataPushEvent(cmd, event, outputJSON)
					if consoleServer != nil {
						consoleServer.recordDataPush(event)
					}
					if webhookDispatcher != nil {
						webhookDispatcher.enqueueDataPush(event)
					}
				},
				OnContactControl: func(event aqua.ContactControlEvent) {
					printContactControlEvent(cmd, event, outputJSON)
					if webhookDispatcher != nil {
						webhookDispatcher.enqueueContactControl(event)
					}
				},
				OnRelayEvent: func(event aqua.RelayEvent) {
					printRelayEvent(cmd, event, outputJSON)
					if consoleServer != nil {
						consoleServer.recordRelayEvent(event)
					}
				},
			})
			if err != nil {
				return err
			}
			defer node.Close()

			var consoleAddr string
			if webConsole {
				var shutdownWebConsole func(context.Context) error
				consoleAddr, consoleServer, shutdownWebConsole, err = startWebConsoleServer(webConsoleConfig{
					ListenAddr:  webConsoleAddr,
					StaticDir:   consoleWebDstDir,
					ListenAddrs: resolvedListenAddrs,
					RelayAddrs:  resolvedRelayAddrs,
					RelayMode:   resolvedRelayMode,
					RelayProbe:  relayProbeEnabled,
				}, svc, node, logger)
				if err != nil {
					return err
				}
				defer func() {
					if shutdownWebConsole == nil {
						return
					}
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = shutdownWebConsole(shutdownCtx)
				}()
				logger.Info("web console server started", "listen", consoleAddr, "static_dir", expandHomePath(consoleWebDstDir))
			}

			if outputJSON {
				payload := map[string]any{
					"status":      "ready",
					"peer_id":     node.PeerID(),
					"node_uuid":   identity.NodeUUID,
					"addresses":   node.AddrStrings(),
					"relay_addrs": resolvedRelayAddrs,
					"relay_mode":  resolvedRelayMode,
					"relay_probe": relayProbeEnabled,
					"webhook_url": resolvedWebhookURL,
				}
				if webConsole {
					payload["web_console"] = true
					payload["web_console_addr"] = consoleAddr
					payload["console_web_dst_dir"] = expandHomePath(consoleWebDstDir)
				}
				_ = writeJSON(cmd.OutOrStdout(), payload)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nnode_uuid: %s\npeer_id: %s\nrelay_mode: %s\nrelay_probe: %t\n", identity.NodeUUID, node.PeerID(), resolvedRelayMode, relayProbeEnabled)
				for _, relayAddr := range resolvedRelayAddrs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "relay: %s\n", relayAddr)
				}
				for _, addr := range node.AddrStrings() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				if webConsole {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "web_console: http://%s\n", consoleAddr)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "console_web_dst_dir: %s\n", expandHomePath(consoleWebDstDir))
				}
				if resolvedWebhookURL != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "webhook: %s\n", resolvedWebhookURL)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for incoming streams... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Listen multiaddr (repeatable), default tries /ip4/0.0.0.0/udp/6372/quic-v1 and /ip4/0.0.0.0/tcp/6372, then falls back to random ports")
	cmd.Flags().StringArrayVar(&relayAddrs, "relay", nil, "Relay multiaddr (repeatable, must end with /p2p/<relay_peer_id>)")
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&webConsole, "web-console", false, "Enable Web Console HTTP server")
	cmd.Flags().StringVar(&webConsoleAddr, "web-console-addr", webConsoleListenDefault, "Web Console HTTP listen address")
	cmd.Flags().StringVar(&consoleWebDstDir, "console-web-dst-dir", "", "Web Console built SPA directory")
	cmd.Flags().StringVar(&webhookURL, "webhook", "", "POST each inbound serve event to this HTTP(S) webhook with exponential retry on failure")
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

func resolveRelayProbeEnabledFromEnv() (bool, error) {
	raw := strings.TrimSpace(os.Getenv(envRelayProbe))
	if raw == "" {
		return false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s=%q (supported: 1|true|yes|on|0|false|no|off)", envRelayProbe, raw)
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

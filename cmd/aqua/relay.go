package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newRelayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Run Aqua relay services",
	}
	cmd.AddCommand(newRelayServeCmd())
	return cmd
}

func newRelayServeCmd() *cobra.Command {
	var listenAddrs []string
	var allowlist []string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a dedicated Circuit Relay v2 service",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			svc := serviceFromCmd(cmd)
			identity, _, err := svc.EnsureIdentity(runCtx, time.Now().UTC())
			if err != nil {
				return err
			}
			priv, err := aqua.ParseIdentityPrivateKey(identity.IdentityPrivEd25519)
			if err != nil {
				return err
			}

			resolvedListenAddrs := normalizeAddressList(listenAddrs)
			if len(resolvedListenAddrs) == 0 {
				resolvedListenAddrs = []string{"/ip4/0.0.0.0/tcp/6371", "/ip4/0.0.0.0/tcp/6372/ws"}
			}
			allowedPeers, err := parseRelayAllowlist(allowlist)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
			acl := relayAllowlistACL{allowed: allowedPeers}
			h, err := libp2p.New(
				libp2p.Identity(priv),
				libp2p.ListenAddrStrings(resolvedListenAddrs...),
				// Dedicated relay mode should always expose the v2 hop protocol,
				// even when AutoNAT reports private/unknown reachability.
				libp2p.ForceReachabilityPublic(),
				libp2p.EnableRelayService(relayv2.WithACL(acl)),
			)
			if err != nil {
				return fmt.Errorf("create relay host: %w", err)
			}
			defer h.Close()
			logger.Info("relay service started", "peer_id", h.ID().String(), "allow_all", len(allowedPeers) == 0)

			addresses, err := hostP2PAddrStrings(h.Addrs(), h.ID())
			if err != nil {
				return err
			}
			addresses = expandAdvertiseAddressesForListenAddrs(addresses, resolvedListenAddrs)
			allowlistOut := sortedAllowlistStrings(allowedPeers)

			if outputJSON {
				_ = writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":      "ready",
					"peer_id":     h.ID().String(),
					"node_uuid":   identity.NodeUUID,
					"addresses":   addresses,
					"allowlist":   allowlistOut,
					"allow_all":   len(allowedPeers) == 0,
					"listen":      resolvedListenAddrs,
					"service":     "relay",
					"relay_mode":  "server",
					"started_at":  time.Now().UTC(),
					"protocol_id": "libp2p.relay/v2",
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nservice: relay\nnode_uuid: %s\npeer_id: %s\n", identity.NodeUUID, h.ID().String())
				if len(allowlistOut) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "allowlist: (empty, allow all)")
				} else {
					for _, id := range allowlistOut {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "allowlist: %s\n", id)
					}
				}
				for _, addr := range addresses {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for relay traffic... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&listenAddrs, "listen", []string{"/ip4/0.0.0.0/tcp/6371", "/ip4/0.0.0.0/tcp/6372/ws"}, "Relay listen multiaddr (repeatable)")
	cmd.Flags().StringArrayVar(&allowlist, "allow-peer", nil, "Allowlist peer id (repeatable, default empty means allow all peers)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status as JSON")
	return cmd
}

type relayAllowlistACL struct {
	allowed map[peer.ID]bool
}

func (a relayAllowlistACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool {
	if len(a.allowed) == 0 {
		return true
	}
	return a.allowed[p]
}

func (a relayAllowlistACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	if len(a.allowed) == 0 {
		return true
	}
	return a.allowed[src] && a.allowed[dest]
}

func parseRelayAllowlist(raw []string) (map[peer.ID]bool, error) {
	normalized := normalizeAddressList(raw)
	if len(normalized) == 0 {
		return map[peer.ID]bool{}, nil
	}
	out := map[peer.ID]bool{}
	for _, item := range normalized {
		id, err := peer.Decode(strings.TrimSpace(item))
		if err != nil {
			return nil, fmt.Errorf("invalid allowlist peer id %q: %w", item, err)
		}
		out[id] = true
	}
	return out, nil
}

func sortedAllowlistStrings(allowed map[peer.ID]bool) []string {
	if len(allowed) == 0 {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for id := range allowed {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

func hostP2PAddrStrings(base []ma.Multiaddr, id peer.ID) ([]string, error) {
	if len(base) == 0 {
		return nil, nil
	}
	p2pComponent, err := ma.NewMultiaddr("/p2p/" + id.String())
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(base))
	for _, addr := range base {
		out = append(out, addr.Encapsulate(p2pComponent).String())
	}
	return normalizeAddressList(out), nil
}

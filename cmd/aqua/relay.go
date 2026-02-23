package main

import (
	"context"
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
	var maxReservations int
	var maxReservationsPerIP int

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
				resolvedListenAddrs = []string{"/ip4/0.0.0.0/tcp/6372", "/ip4/0.0.0.0/udp/6372/quic-v1"}
			}
			allowedPeers, err := parseRelayAllowlist(allowlist)
			if err != nil {
				return err
			}
			resources, err := resolveRelayResources(maxReservations, maxReservationsPerIP)
			if err != nil {
				return err
			}

			logger, err := loggerFromCmd(cmd)
			if err != nil {
				return err
			}
			logger.Debug(
				"relay serve config",
				"listen_addrs", resolvedListenAddrs,
				"allowlist_count", len(allowedPeers),
				"max_reservations", resources.MaxReservations,
				"max_reservations_per_ip", resources.MaxReservationsPerIP,
			)
			acl := relayAllowlistACL{allowed: allowedPeers, logger: logger}
			h, err := libp2p.New(
				libp2p.Identity(priv),
				libp2p.ListenAddrStrings(resolvedListenAddrs...),
				// Dedicated relay mode should always expose the v2 hop protocol,
				// even when AutoNAT reports private/unknown reachability.
				libp2p.ForceReachabilityPublic(),
				libp2p.EnableRelayService(
					relayv2.WithACL(acl),
					relayv2.WithResources(resources),
				),
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
					"status":     "ready",
					"peer_id":    h.ID().String(),
					"node_uuid":  identity.NodeUUID,
					"addresses":  addresses,
					"allowlist":  allowlistOut,
					"allow_all":  len(allowedPeers) == 0,
					"listen":     resolvedListenAddrs,
					"service":    "relay",
					"relay_mode": "server",
					"resources": map[string]any{
						"max_reservations":        resources.MaxReservations,
						"max_reservations_per_ip": resources.MaxReservationsPerIP,
					},
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
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "max_reservations: %d\n", resources.MaxReservations)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "max_reservations_per_ip: %d\n", resources.MaxReservationsPerIP)
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for relay traffic... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&listenAddrs, "listen", []string{"/ip4/0.0.0.0/tcp/6372", "/ip4/0.0.0.0/udp/6372/quic-v1"}, "Relay listen multiaddr (repeatable)")
	cmd.Flags().StringArrayVar(&allowlist, "allow-peer", nil, "Allowlist peer id (repeatable, default empty means allow all peers)")
	cmd.Flags().IntVar(&maxReservations, "max-reservations", 512, "Maximum number of active relay reservations")
	cmd.Flags().IntVar(&maxReservationsPerIP, "max-reservations-per-ip", 4, "Maximum number of relay reservations per source IP")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status as JSON")
	return cmd
}

func resolveRelayResources(maxReservations int, maxReservationsPerIP int) (relayv2.Resources, error) {
	if maxReservations <= 0 {
		return relayv2.Resources{}, fmt.Errorf("invalid --max-reservations %d (must be > 0)", maxReservations)
	}
	if maxReservationsPerIP <= 0 {
		return relayv2.Resources{}, fmt.Errorf("invalid --max-reservations-per-ip %d (must be > 0)", maxReservationsPerIP)
	}
	if maxReservationsPerIP > maxReservations {
		return relayv2.Resources{}, fmt.Errorf(
			"invalid relay limits: --max-reservations-per-ip (%d) must be <= --max-reservations (%d)",
			maxReservationsPerIP,
			maxReservations,
		)
	}
	resources := relayv2.DefaultResources()
	resources.MaxReservations = maxReservations
	resources.MaxReservationsPerIP = maxReservationsPerIP
	return resources, nil
}

type relayAllowlistACL struct {
	allowed map[peer.ID]bool
	logger  *slog.Logger
}

func (a relayAllowlistACL) AllowReserve(p peer.ID, addr ma.Multiaddr) bool {
	allowed := len(a.allowed) == 0 || a.allowed[p]
	if a.logger != nil {
		level := slog.LevelInfo
		if !allowed {
			level = slog.LevelWarn
		}
		a.logger.Log(
			context.Background(),
			level,
			"relay reservation request",
			"peer_id", p.String(),
			"source_addr", relayMultiaddrString(addr),
			"allowed", allowed,
		)
	}
	return allowed
}

func (a relayAllowlistACL) AllowConnect(src peer.ID, addr ma.Multiaddr, dest peer.ID) bool {
	allowed := len(a.allowed) == 0 || (a.allowed[src] && a.allowed[dest])
	if a.logger != nil {
		level := slog.LevelInfo
		if !allowed {
			level = slog.LevelWarn
		}
		a.logger.Log(
			context.Background(),
			level,
			"relay circuit request",
			"src_peer_id", src.String(),
			"dest_peer_id", dest.String(),
			"source_addr", relayMultiaddrString(addr),
			"allowed", allowed,
		)
	}
	return allowed
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

func relayMultiaddrString(addr ma.Multiaddr) string {
	if addr == nil {
		return ""
	}
	return strings.TrimSpace(addr.String())
}

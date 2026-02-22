package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func contactDisplayLabel(contact aqua.Contact) string {
	label := strings.TrimSpace(contact.DisplayName)
	if label != "" {
		return label
	}
	return strings.TrimSpace(contact.Nickname)
}

func extractPeerIDFromDialAddress(rawAddress string) (string, error) {
	address := strings.TrimSpace(rawAddress)
	if address == "" {
		return "", fmt.Errorf("address is required")
	}
	maddr, err := ma.NewMultiaddr(address)
	if err != nil {
		return "", fmt.Errorf("invalid address %q: %w", address, err)
	}
	_, last := ma.SplitLast(maddr)
	if last == nil || last.Protocol().Code != ma.P_P2P {
		return "", fmt.Errorf("address %q must end with /p2p/<peer_id>", address)
	}
	peerID := strings.TrimSpace(last.Value())
	if peerID == "" {
		return "", fmt.Errorf("address %q has empty /p2p peer id", address)
	}
	if _, err := peer.Decode(peerID); err != nil {
		return "", fmt.Errorf("address %q has invalid /p2p peer id: %w", address, err)
	}
	return peerID, nil
}

func newDialNode(cmd *cobra.Command) (*aqua.Node, error) {
	return newDialNodeWithRelayMode(cmd, "")
}

func newDialNodeWithRelayMode(cmd *cobra.Command, relayMode string) (*aqua.Node, error) {
	svc := serviceFromCmd(cmd)
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	return aqua.NewNode(cmd.Context(), svc, aqua.NodeOptions{
		DialOnly:  true,
		RelayMode: relayMode,
		Logger:    logger,
	})
}

func resolveCardExportAddressesForCommand(
	ctx context.Context,
	svc *aqua.Service,
	explicit []string,
	configuredListenAddrs []string,
	in io.Reader,
	out io.Writer,
) ([]string, error) {
	return resolveCardExportAddressesWithPrompt(ctx, svc, explicit, configuredListenAddrs, in, out, isInteractiveTerminal(in, out))
}

func resolveCardExportAddressesWithPrompt(
	ctx context.Context,
	svc *aqua.Service,
	explicit []string,
	configuredListenAddrs []string,
	in io.Reader,
	out io.Writer,
	interactive bool,
) ([]string, error) {
	normalizedExplicit := normalizeAddressList(explicit)
	if len(normalizedExplicit) > 0 {
		return normalizedExplicit, nil
	}
	normalizedConfigured := normalizeAddressList(configuredListenAddrs)
	if len(normalizedConfigured) == 0 {
		return nil, fmt.Errorf("at least one --address is required (or set --listen)")
	}

	identity, ok, err := svc.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("identity not found; run `aqua init`")
	}
	resolved, err := appendPeerIDToAddresses(normalizedConfigured, identity.PeerID)
	if err != nil {
		return nil, err
	}
	resolved = expandConfiguredDialAddresses(resolved)
	return selectCardExportDialAddresses(resolved, in, out, interactive)
}

func appendPeerIDToAddresses(addresses []string, peerID string) ([]string, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return nil, fmt.Errorf("empty local peer_id")
	}
	peerComponent, err := ma.NewMultiaddr("/p2p/" + peerID)
	if err != nil {
		return nil, fmt.Errorf("build /p2p component: %w", err)
	}

	out := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, raw := range addresses {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid configured address %q: %w", addr, err)
		}
		if _, last := ma.SplitLast(maddr); last != nil && last.Protocol().Code == ma.P_P2P {
			if strings.TrimSpace(last.Value()) != peerID {
				return nil, fmt.Errorf("configured address %q has mismatched /p2p/%s (local peer_id=%s)", addr, last.Value(), peerID)
			}
			canonical := maddr.String()
			if !seen[canonical] {
				seen[canonical] = true
				out = append(out, canonical)
			}
			continue
		}

		withPeer := maddr.Encapsulate(peerComponent).String()
		if seen[withPeer] {
			continue
		}
		seen[withPeer] = true
		out = append(out, withPeer)
	}
	return out, nil
}

func expandConfiguredDialAddresses(addresses []string) []string {
	localIPs, err := discoverLocalInterfaceIPs()
	if err != nil || len(localIPs) == 0 {
		return normalizeAddressList(addresses)
	}
	return expandConfiguredDialAddressesWithIPs(addresses, localIPs)
}

func expandAdvertiseAddressesForListenAddrs(addresses []string, listenAddrs []string) []string {
	normalized := normalizeAddressList(addresses)
	if !hasWildcardListenAddress(listenAddrs) {
		return normalized
	}
	localIPs, err := discoverLocalInterfaceIPs()
	if err != nil || len(localIPs) == 0 {
		return normalized
	}
	return expandAdvertiseAddressesWithIPs(normalized, localIPs)
}

func hasWildcardListenAddress(listenAddrs []string) bool {
	for _, raw := range normalizeAddressList(listenAddrs) {
		addr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}
		if value, err := addr.ValueForProtocol(ma.P_IP4); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				return true
			}
		}
		if value, err := addr.ValueForProtocol(ma.P_IP6); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				return true
			}
		}
	}
	return false
}

func expandAdvertiseAddressesWithIPs(addresses []string, localIPs []net.IP) []string {
	out := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}

	for _, raw := range normalizeAddressList(addresses) {
		add(raw)
		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}

		if _, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
			for _, local := range localIPs {
				v4 := local.To4()
				if v4 == nil {
					continue
				}
				replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP4, v4.String())
				if err != nil {
					continue
				}
				add(replaced.String())
			}
		}
		if _, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
			for _, local := range localIPs {
				if local.To4() != nil || local.To16() == nil {
					continue
				}
				replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP6, local.String())
				if err != nil {
					continue
				}
				add(replaced.String())
			}
		}
	}
	return out
}

func expandConfiguredDialAddressesWithIPs(addresses []string, localIPs []net.IP) []string {
	out := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}

	for _, raw := range normalizeAddressList(addresses) {
		add(raw)

		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}
		if value, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				for _, local := range localIPs {
					v4 := local.To4()
					if v4 == nil {
						continue
					}
					replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP4, v4.String())
					if err != nil {
						continue
					}
					add(replaced.String())
				}
			}
		}
		if value, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				for _, local := range localIPs {
					if local.To4() != nil || local.To16() == nil {
						continue
					}
					replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP6, local.String())
					if err != nil {
						continue
					}
					add(replaced.String())
				}
			}
		}
	}
	return out
}

func replaceMultiaddrIPComponent(addr ma.Multiaddr, protoCode int, value string) (ma.Multiaddr, error) {
	if addr == nil {
		return nil, fmt.Errorf("nil multiaddr")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return addr, nil
	}
	raw := addr.String()
	updated := raw
	switch protoCode {
	case ma.P_IP4:
		if strings.Contains(raw, "/ip4/") {
			parts := strings.Split(raw, "/")
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "ip4" {
					parts[i+1] = value
					updated = strings.Join(parts, "/")
					break
				}
			}
		}
	case ma.P_IP6:
		if strings.Contains(raw, "/ip6/") {
			parts := strings.Split(raw, "/")
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "ip6" {
					parts[i+1] = value
					updated = strings.Join(parts, "/")
					break
				}
			}
		}
	default:
		return addr, nil
	}
	if updated == raw {
		return addr, nil
	}
	rebuilt, err := ma.NewMultiaddr(updated)
	if err != nil {
		return nil, err
	}
	return rebuilt, nil
}

func discoverLocalInterfaceIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, 8)
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := interfaceAddrIP(addr)
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			key := ip.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ip)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsLoopback() != out[j].IsLoopback() {
			return !out[i].IsLoopback()
		}
		isV4i := out[i].To4() != nil
		isV4j := out[j].To4() != nil
		if isV4i != isV4j {
			return isV4i
		}
		return out[i].String() < out[j].String()
	})
	return out, nil
}

func interfaceAddrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		if v == nil || v.IP == nil {
			return nil
		}
		return v.IP
	case *net.IPAddr:
		if v == nil || v.IP == nil {
			return nil
		}
		return v.IP
	default:
		return nil
	}
}

func selectCardExportDialAddresses(addresses []string, in io.Reader, out io.Writer, interactive bool) ([]string, error) {
	valid, invalid := classifyCardExportDialAddresses(addresses)
	if len(valid) == 0 {
		if len(invalid) == 0 {
			return nil, fmt.Errorf("no dialable addresses available (provide --address explicitly)")
		}
		reasons := make([]string, 0, len(invalid))
		for _, item := range invalid {
			reasons = append(reasons, fmt.Sprintf("%s (%s)", item.Address, item.Reason))
		}
		return nil, fmt.Errorf("no dialable addresses available from --listen: %s", strings.Join(reasons, "; "))
	}
	if !interactive {
		if len(valid) == 1 {
			return []string{valid[0]}, nil
		}
		return nil, fmt.Errorf("multiple dialable addresses found; pass --address explicitly or run in an interactive terminal")
	}

	if out == nil {
		out = os.Stderr
	}
	if in == nil {
		in = os.Stdin
	}
	_, _ = fmt.Fprintln(out, "No --address provided. Select one dialable address for contact card:")
	for i, addr := range valid {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", i+1, addr)
	}
	if len(invalid) > 0 {
		_, _ = fmt.Fprintln(out, "Ignored non-dialable addresses:")
		for _, item := range invalid {
			_, _ = fmt.Fprintf(out, "  - %s (%s)\n", item.Address, item.Reason)
		}
	}

	reader := bufio.NewReader(in)
	for {
		_, _ = fmt.Fprintf(out, "Select address [1-%d] (default 1): ", len(valid))
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read selection: %w", err)
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return []string{valid[0]}, nil
		}
		if strings.EqualFold(choice, "q") || strings.EqualFold(choice, "quit") {
			return nil, fmt.Errorf("card export cancelled")
		}
		index, parseErr := strconv.Atoi(choice)
		if parseErr == nil && index >= 1 && index <= len(valid) {
			return []string{valid[index-1]}, nil
		}
		_, _ = fmt.Fprintf(out, "Invalid selection: %q\n", choice)
		if err == io.EOF {
			return nil, fmt.Errorf("invalid selection: %q", choice)
		}
	}
}

type invalidDialAddress struct {
	Address string
	Reason  string
}

func classifyCardExportDialAddresses(addresses []string) ([]string, []invalidDialAddress) {
	valid := make([]string, 0, len(addresses))
	invalid := make([]invalidDialAddress, 0, len(addresses))
	for _, raw := range addresses {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		reason := dialAddressInvalidReason(address)
		if reason != "" {
			invalid = append(invalid, invalidDialAddress{Address: address, Reason: reason})
			continue
		}
		valid = append(valid, address)
	}
	return valid, invalid
}

func dialAddressInvalidReason(address string) string {
	maddr, err := ma.NewMultiaddr(address)
	if err != nil {
		return "invalid multiaddr"
	}
	if value, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return "ip4 unspecified"
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return "ip6 unspecified"
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_TCP); err == nil && strings.TrimSpace(value) == "0" {
		return "tcp port 0"
	}
	if value, err := maddr.ValueForProtocol(ma.P_UDP); err == nil && strings.TrimSpace(value) == "0" {
		return "udp port 0"
	}
	return ""
}

func isInteractiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	inInfo, err := inFile.Stat()
	if err != nil {
		return false
	}
	outInfo, err := outFile.Stat()
	if err != nil {
		return false
	}
	return (inInfo.Mode()&os.ModeCharDevice) != 0 && (outInfo.Mode()&os.ModeCharDevice) != 0
}

func normalizeAddressList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func splitDirectAndRelayAddresses(addresses []string) ([]string, []string) {
	direct := make([]string, 0, len(addresses))
	relay := make([]string, 0, len(addresses))
	for _, raw := range normalizeAddressList(addresses) {
		if strings.Contains(strings.ToLower(raw), "/p2p-circuit") {
			relay = append(relay, raw)
			continue
		}
		direct = append(direct, raw)
	}
	return direct, relay
}

func buildRelayAdvertiseAddresses(relayEndpoints []string, targetPeerID string) ([]string, error) {
	targetPeerID = strings.TrimSpace(targetPeerID)
	if targetPeerID == "" {
		return nil, fmt.Errorf("target peer id is required")
	}
	if _, err := peer.Decode(targetPeerID); err != nil {
		return nil, fmt.Errorf("invalid target peer id %q: %w", targetPeerID, err)
	}
	targetComponent, err := ma.NewMultiaddr("/p2p/" + targetPeerID)
	if err != nil {
		return nil, fmt.Errorf("build target peer component: %w", err)
	}
	circuitComponent, err := ma.NewMultiaddr("/p2p-circuit")
	if err != nil {
		return nil, fmt.Errorf("build relay circuit component: %w", err)
	}

	out := make([]string, 0, len(relayEndpoints))
	for _, raw := range normalizeAddressList(relayEndpoints) {
		if strings.Contains(strings.ToLower(raw), "/p2p-circuit") {
			return nil, fmt.Errorf("relay endpoint must not include /p2p-circuit: %s", raw)
		}
		info, err := peer.AddrInfoFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid relay endpoint %q: %w", raw, err)
		}
		if info == nil || info.ID == "" {
			return nil, fmt.Errorf("invalid relay endpoint %q: missing relay peer id", raw)
		}
		relayPeerComponent, err := ma.NewMultiaddr("/p2p/" + info.ID.String())
		if err != nil {
			return nil, fmt.Errorf("build relay peer component for %q: %w", raw, err)
		}
		for _, relayTransportAddr := range info.Addrs {
			out = append(out, relayTransportAddr.Encapsulate(relayPeerComponent).Encapsulate(circuitComponent).Encapsulate(targetComponent).String())
		}
	}
	return normalizeAddressList(out), nil
}

func serviceFromCmd(cmd *cobra.Command) *aqua.Service {
	dir, _ := cmd.Flags().GetString("dir")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = defaultAquaDir()
	}
	dir = expandHomePath(dir)
	store := aqua.NewFileStore(dir)
	return aqua.NewService(store)
}

func defaultAquaDir() string {
	if v := strings.TrimSpace(os.Getenv("AQUA_DIR")); v != "" {
		return expandHomePath(v)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".aqua"
	}
	return filepath.Join(home, ".aqua")
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func readInputFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func messageEnvelopeKey(messageID string) string {
	return "msg:" + idempotencyToken(messageID)
}

func idempotencyToken(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

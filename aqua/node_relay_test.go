package aqua

import (
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNormalizeRelayMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default empty", input: "", want: RelayModeAuto},
		{name: "auto", input: "auto", want: RelayModeAuto},
		{name: "off", input: "off", want: RelayModeOff},
		{name: "required", input: "required", want: RelayModeRequired},
		{name: "trimmed", input: "  auto  ", want: RelayModeAuto},
		{name: "invalid", input: "always", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeRelayMode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeRelayMode(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRelayMode(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRelayMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDialAddressSetsForMode(t *testing.T) {
	t.Parallel()

	direct := []string{"/ip4/127.0.0.1/tcp/6372/p2p/peerA"}
	relay := []string{"/dns4/relay.example.com/tcp/6372/p2p/relayPeer/p2p-circuit/p2p/peerA"}

	t.Run("auto uses direct then relay", func(t *testing.T) {
		t.Parallel()

		sets := dialAddressSetsForMode(RelayModeAuto, direct, relay)
		if len(sets) != 2 {
			t.Fatalf("sets length mismatch: got %d want 2", len(sets))
		}
		if sets[0].Path != "direct" || len(sets[0].Addresses) != 1 {
			t.Fatalf("unexpected first set: %+v", sets[0])
		}
		if sets[1].Path != "relay" || len(sets[1].Addresses) != 1 {
			t.Fatalf("unexpected second set: %+v", sets[1])
		}
	})

	t.Run("off uses direct only", func(t *testing.T) {
		t.Parallel()

		sets := dialAddressSetsForMode(RelayModeOff, direct, relay)
		if len(sets) != 1 || sets[0].Path != "direct" {
			t.Fatalf("unexpected sets: %+v", sets)
		}
	})

	t.Run("required uses relay only", func(t *testing.T) {
		t.Parallel()

		sets := dialAddressSetsForMode(RelayModeRequired, direct, relay)
		if len(sets) != 1 || sets[0].Path != "relay" {
			t.Fatalf("unexpected sets: %+v", sets)
		}
	})

	t.Run("off without direct returns none", func(t *testing.T) {
		t.Parallel()

		sets := dialAddressSetsForMode(RelayModeOff, nil, relay)
		if len(sets) != 0 {
			t.Fatalf("expected empty sets, got %+v", sets)
		}
	})

	t.Run("required without relay returns none", func(t *testing.T) {
		t.Parallel()

		sets := dialAddressSetsForMode(RelayModeRequired, direct, nil)
		if len(sets) != 0 {
			t.Fatalf("expected empty sets, got %+v", sets)
		}
	})
}

func TestRelayPeerIDFromCircuitAddress(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 10, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}

	addr := fmt.Sprintf("/dns4/relay.example.com/tcp/6372/p2p/%s/p2p-circuit/p2p/%s", relayIdentity.PeerID, targetIdentity.PeerID)
	got := relayPeerIDFromCircuitAddress(addr)
	want := relayIdentity.PeerID
	if got != want {
		t.Fatalf("relayPeerIDFromCircuitAddress() = %q, want %q", got, want)
	}
	if relayPeerIDFromCircuitAddress("/ip4/127.0.0.1/tcp/6372/p2p/peerA") != "" {
		t.Fatalf("expected empty relay peer id for non-circuit address")
	}
}

func TestParseRelayAddrInfos(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 10, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	raw := fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", relayIdentity.PeerID)

	infos, err := parseRelayAddrInfos([]string{raw, raw})
	if err != nil {
		t.Fatalf("parseRelayAddrInfos() error = %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("relay info count mismatch: got %d want 1", len(infos))
	}
	if infos[0].ID.String() != relayIdentity.PeerID {
		t.Fatalf("relay peer mismatch: got %s want %s", infos[0].ID.String(), relayIdentity.PeerID)
	}
	if len(infos[0].Addrs) != 1 {
		t.Fatalf("relay addr count mismatch: got %d want 1", len(infos[0].Addrs))
	}
}

func TestParseRelayAddrInfos_RejectsCircuitEndpoint(t *testing.T) {
	t.Parallel()

	_, err := parseRelayAddrInfos([]string{"/dns4/relay.example.com/tcp/6372/p2p/12D3KooWRelayPeer/p2p-circuit"})
	if err == nil {
		t.Fatalf("expected relay endpoint with /p2p-circuit to be rejected")
	}
}

func TestRejectRelayInfosForLocalPeerID(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 10, 10, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	otherIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 10, 10, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(other) error = %v", err)
	}
	localPeerID, err := peer.Decode(relayIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(local) error = %v", err)
	}

	infos, err := parseRelayAddrInfos([]string{
		fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", otherIdentity.PeerID),
	})
	if err != nil {
		t.Fatalf("parseRelayAddrInfos() error = %v", err)
	}

	if err := rejectRelayInfosForLocalPeerID(infos, localPeerID); err == nil {
		t.Fatalf("expected self relay peer_id to be rejected")
	}

	otherPeerID, err := peer.Decode(otherIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(other) error = %v", err)
	}
	otherInfos, err := parseRelayAddrInfos([]string{
		fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", relayIdentity.PeerID),
	})
	if err != nil {
		t.Fatalf("parseRelayAddrInfos(otherInfos) error = %v", err)
	}
	if err := rejectRelayInfosForLocalPeerID(otherInfos, otherPeerID); err != nil {
		t.Fatalf("rejectRelayInfosForLocalPeerID() unexpected error = %v", err)
	}
}

func TestCanonicalRelayCircuitBaseAddr(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 23, 10, 20, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 23, 10, 20, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}
	relayID, err := peer.Decode(relayIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(relay) error = %v", err)
	}

	t.Run("adds missing p2p-circuit", func(t *testing.T) {
		t.Parallel()

		raw := fmt.Sprintf("/ip4/43.206.8.204/udp/6372/quic-v1/p2p/%s", relayIdentity.PeerID)
		got, err := canonicalRelayCircuitBaseAddr(raw, relayID)
		if err != nil {
			t.Fatalf("canonicalRelayCircuitBaseAddr() error = %v", err)
		}
		want := fmt.Sprintf("/ip4/43.206.8.204/udp/6372/quic-v1/p2p/%s/p2p-circuit", relayIdentity.PeerID)
		if got != want {
			t.Fatalf("canonical relay addr mismatch: got %q want %q", got, want)
		}
	})

	t.Run("drops target suffix after circuit", func(t *testing.T) {
		t.Parallel()

		raw := fmt.Sprintf(
			"/dns4/relay.example.com/tcp/6372/p2p/%s/p2p-circuit/p2p/%s",
			relayIdentity.PeerID,
			targetIdentity.PeerID,
		)
		got, err := canonicalRelayCircuitBaseAddr(raw, relayID)
		if err != nil {
			t.Fatalf("canonicalRelayCircuitBaseAddr() error = %v", err)
		}
		want := fmt.Sprintf("/dns4/relay.example.com/tcp/6372/p2p/%s/p2p-circuit", relayIdentity.PeerID)
		if got != want {
			t.Fatalf("canonical relay addr mismatch: got %q want %q", got, want)
		}
	})
}

func TestNormalizeRelayReservationAddrs(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 23, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	relayID, err := peer.Decode(relayIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(relay) error = %v", err)
	}

	raw := []string{
		fmt.Sprintf("/ip4/18.179.41.50/tcp/6372/p2p/%s", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/18.179.41.50/udp/6372/quic-v1/p2p/%s", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/18.179.41.50/tcp/6372/p2p/%s/p2p-circuit/p2p/12D3KooWTarget", relayIdentity.PeerID),
	}
	got := normalizeRelayReservationAddrs(raw, relayID, nil)
	gotSet := map[string]bool{}
	for _, addr := range got {
		gotSet[addr] = true
	}
	expect := []string{
		fmt.Sprintf("/ip4/18.179.41.50/tcp/6372/p2p/%s/p2p-circuit", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/18.179.41.50/udp/6372/quic-v1/p2p/%s/p2p-circuit", relayIdentity.PeerID),
	}
	if len(gotSet) != len(expect) {
		t.Fatalf("normalized relay addr count mismatch: got %d want %d (%v)", len(gotSet), len(expect), got)
	}
	for _, want := range expect {
		if !gotSet[want] {
			t.Fatalf("missing normalized relay addr %q in %v", want, got)
		}
	}
}

func TestRelayAdvertiseBaseAddrs(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 23, 10, 40, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 23, 10, 40, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}
	relayID, err := peer.Decode(relayIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(relay) error = %v", err)
	}

	infos, err := parseRelayAddrInfos([]string{
		fmt.Sprintf("/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/%s", relayIdentity.PeerID),
		fmt.Sprintf("/dns4/aqua-relay.mistermorph.com/udp/6372/quic-v1/p2p/%s", relayIdentity.PeerID),
	})
	if err != nil {
		t.Fatalf("parseRelayAddrInfos() error = %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("relay info count mismatch: got %d want 1", len(infos))
	}

	reservation := []string{
		fmt.Sprintf("/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/%s", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/43.206.8.204/tcp/6372/p2p/%s/p2p-circuit/p2p/%s", relayIdentity.PeerID, targetIdentity.PeerID),
	}

	got := relayAdvertiseBaseAddrs(infos[0], reservation, nil)
	gotSet := map[string]bool{}
	for _, addr := range got {
		gotSet[addr] = true
	}

	expect := []string{
		fmt.Sprintf("/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/%s/p2p-circuit", relayIdentity.PeerID),
		fmt.Sprintf("/dns4/aqua-relay.mistermorph.com/udp/6372/quic-v1/p2p/%s/p2p-circuit", relayIdentity.PeerID),
		fmt.Sprintf("/ip4/43.206.8.204/tcp/6372/p2p/%s/p2p-circuit", relayIdentity.PeerID),
	}
	if len(gotSet) != len(expect) {
		t.Fatalf("relay advertise addr count mismatch: got %d want %d (%v)", len(gotSet), len(expect), got)
	}
	for _, want := range expect {
		if !gotSet[want] {
			t.Fatalf("missing relay advertise addr %q in %v", want, got)
		}
	}

	onlyConfigured := relayAdvertiseBaseAddrs(infos[0], nil, nil)
	if len(onlyConfigured) != 2 {
		t.Fatalf("configured-only relay advertise addr count mismatch: got %d want 2 (%v)", len(onlyConfigured), onlyConfigured)
	}

	emptyConfigured := relayAdvertiseBaseAddrs(peer.AddrInfo{ID: relayID}, reservation, nil)
	if len(emptyConfigured) != 2 {
		t.Fatalf("reservation-only relay advertise addr count mismatch: got %d want 2 (%v)", len(emptyConfigured), emptyConfigured)
	}
}

func TestRelayReservationRenewDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 23, 2, 0, 0, 0, time.UTC)

	t.Run("uses expiration with lead time", func(t *testing.T) {
		t.Parallel()

		got := relayReservationRenewDelay(now, now.Add(time.Hour))
		want := 55 * time.Minute
		if got != want {
			t.Fatalf("relayReservationRenewDelay() = %s, want %s", got, want)
		}
	})

	t.Run("enforces min interval near expiration", func(t *testing.T) {
		t.Parallel()

		got := relayReservationRenewDelay(now, now.Add(90*time.Second))
		if got != relayRenewMinInterval {
			t.Fatalf("relayReservationRenewDelay() = %s, want %s", got, relayRenewMinInterval)
		}
	})

	t.Run("uses fallback interval without expiration", func(t *testing.T) {
		t.Parallel()

		got := relayReservationRenewDelay(now, time.Time{})
		if got != relayRenewFallback {
			t.Fatalf("relayReservationRenewDelay() = %s, want %s", got, relayRenewFallback)
		}
	})
}

func TestNextRelayReservationRetryDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{name: "zero uses min", current: 0, want: relayRetryMinInterval},
		{name: "negative uses min", current: -time.Second, want: relayRetryMinInterval},
		{name: "doubles", current: relayRetryMinInterval, want: 10 * time.Second},
		{name: "caps at max", current: 70 * time.Second, want: relayRetryMaxInterval},
		{name: "max stays max", current: relayRetryMaxInterval, want: relayRetryMaxInterval},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nextRelayReservationRetryDelay(tt.current)
			if got != tt.want {
				t.Fatalf("nextRelayReservationRetryDelay() = %s, want %s", got, tt.want)
			}
		})
	}
}

package aqua

import (
	"fmt"
	"testing"
	"time"
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

	direct := []string{"/ip4/127.0.0.1/tcp/6371/p2p/peerA"}
	relay := []string{"/dns4/relay.example.com/tcp/6371/p2p/relayPeer/p2p-circuit/p2p/peerA"}

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

	addr := fmt.Sprintf("/dns4/relay.example.com/tcp/6371/p2p/%s/p2p-circuit/p2p/%s", relayIdentity.PeerID, targetIdentity.PeerID)
	got := relayPeerIDFromCircuitAddress(addr)
	want := relayIdentity.PeerID
	if got != want {
		t.Fatalf("relayPeerIDFromCircuitAddress() = %q, want %q", got, want)
	}
	if relayPeerIDFromCircuitAddress("/ip4/127.0.0.1/tcp/6371/p2p/peerA") != "" {
		t.Fatalf("expected empty relay peer id for non-circuit address")
	}
}

package aqua

import (
	"testing"

	libp2p "github.com/libp2p/go-libp2p"
)

func TestLibp2pNetworkOptions_DialOnlyKeepsRelayTransport(t *testing.T) {
	t.Parallel()

	var cfg libp2p.Config
	if err := cfg.Apply(libp2pNetworkOptions(true, nil)...); err != nil {
		t.Fatalf("cfg.Apply(dialOnly) error = %v", err)
	}
	if len(cfg.ListenAddrs) != 0 {
		t.Fatalf("dial-only listen addrs mismatch: got %d want 0", len(cfg.ListenAddrs))
	}
	if !cfg.RelayCustom || !cfg.Relay {
		t.Fatalf("dial-only relay transport must stay enabled (RelayCustom=%v Relay=%v)", cfg.RelayCustom, cfg.Relay)
	}
}

func TestLibp2pNetworkOptions_ListenModeUsesConfiguredAddresses(t *testing.T) {
	t.Parallel()

	listen := []string{
		"/ip4/0.0.0.0/tcp/6371",
		"/ip4/0.0.0.0/tcp/6372/ws",
	}
	var cfg libp2p.Config
	if err := cfg.Apply(libp2pNetworkOptions(false, listen)...); err != nil {
		t.Fatalf("cfg.Apply(listen) error = %v", err)
	}
	if len(cfg.ListenAddrs) != len(listen) {
		t.Fatalf("listen addr count mismatch: got %d want %d", len(cfg.ListenAddrs), len(listen))
	}
	for i := range listen {
		if got := cfg.ListenAddrs[i].String(); got != listen[i] {
			t.Fatalf("listen addr[%d] mismatch: got %q want %q", i, got, listen[i])
		}
	}
}

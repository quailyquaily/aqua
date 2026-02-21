package aqua

import (
	"net"
	"testing"
)

func TestHasWildcardListenAddress(t *testing.T) {
	t.Parallel()

	if !hasWildcardListenAddress([]string{"/ip4/0.0.0.0/tcp/6371"}) {
		t.Fatalf("expected wildcard ip4 listen address to be detected")
	}
	if !hasWildcardListenAddress([]string{"/ip6/::/udp/6371/quic-v1"}) {
		t.Fatalf("expected wildcard ip6 listen address to be detected")
	}
	if hasWildcardListenAddress([]string{"/ip4/192.168.1.10/tcp/6371"}) {
		t.Fatalf("did not expect specific listen address to be wildcard")
	}
}

func TestExpandAdvertiseAddresses_AddsInterfaceVariants(t *testing.T) {
	t.Parallel()

	base := []string{
		"/ip4/192.168.1.10/tcp/6371",
		"/ip4/192.168.1.10/udp/6371/quic-v1",
	}
	localIPs := []net.IP{
		net.ParseIP("192.168.1.10"),
		net.ParseIP("100.64.0.8"),
		net.ParseIP("127.0.0.1"),
	}

	got := expandAdvertiseAddresses(base, localIPs)
	m := make(map[string]bool, len(got))
	for _, addr := range got {
		m[addr] = true
	}

	expected := []string{
		"/ip4/192.168.1.10/tcp/6371",
		"/ip4/192.168.1.10/udp/6371/quic-v1",
		"/ip4/100.64.0.8/tcp/6371",
		"/ip4/100.64.0.8/udp/6371/quic-v1",
	}
	for _, addr := range expected {
		if !m[addr] {
			t.Fatalf("missing expanded address %q in %v", addr, got)
		}
	}
}

func TestExpandAdvertiseAddresses_DedupesResults(t *testing.T) {
	t.Parallel()

	base := []string{
		"/ip4/100.64.0.8/tcp/6371",
		"/ip4/100.64.0.8/tcp/6371",
	}
	localIPs := []net.IP{
		net.ParseIP("100.64.0.8"),
		net.ParseIP("100.64.0.8"),
	}

	got := expandAdvertiseAddresses(base, localIPs)
	if len(got) != 1 {
		t.Fatalf("expected deduped addresses length=1, got %d (%v)", len(got), got)
	}
}

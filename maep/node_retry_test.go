package maep

import "testing"

func TestShouldRetryAfterUnsupported(t *testing.T) {
	if !shouldRetryAfterUnsupported("ERR_UNSUPPORTED_PROTOCOL", false) {
		t.Fatalf("expected retry for unsupported protocol on first attempt")
	}
	if !shouldRetryAfterUnsupported(" err_unsupported_protocol ", false) {
		t.Fatalf("expected case-insensitive retry for unsupported protocol")
	}
	if shouldRetryAfterUnsupported("ERR_METHOD_NOT_ALLOWED", false) {
		t.Fatalf("unexpected retry for unrelated protocol symbol")
	}
	if shouldRetryAfterUnsupported("ERR_UNSUPPORTED_PROTOCOL", true) {
		t.Fatalf("unexpected retry after already retried")
	}
}

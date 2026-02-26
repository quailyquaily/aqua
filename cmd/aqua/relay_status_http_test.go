package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/quailyquaily/aqua/aqua"
	"github.com/quailyquaily/aqua/internal/fsstore"
)

func TestRelayStatusTrackerReservationLifecycle(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 2, 25, 8, 0, 0, 0, time.UTC)
	tracker := newRelayStatusTracker(startedAt)
	tracker.recordReservationAllowed(false, startedAt.Add(1*time.Minute))
	tracker.recordReservationAllowed(true, startedAt.Add(2*time.Minute))
	tracker.recordReservationClosed(1, startedAt.Add(3*time.Minute))

	snapshot := tracker.snapshot(startedAt.Add(4 * time.Minute))
	if got := snapshot.Renewal.ActiveReservations; got != 0 {
		t.Fatalf("ActiveReservations = %d, want 0", got)
	}
	if got := snapshot.Renewal.OpenedTotal; got != 1 {
		t.Fatalf("OpenedTotal = %d, want 1", got)
	}
	if got := snapshot.Renewal.RenewedTotal; got != 1 {
		t.Fatalf("RenewedTotal = %d, want 1", got)
	}
	if got := snapshot.Renewal.ClosedTotal; got != 1 {
		t.Fatalf("ClosedTotal = %d, want 1", got)
	}
	wantRenewedAt := startedAt.Add(2 * time.Minute)
	if snapshot.Renewal.LastRenewedAt == nil || !snapshot.Renewal.LastRenewedAt.Equal(wantRenewedAt) {
		t.Fatalf("LastRenewedAt = %v, want %s", snapshot.Renewal.LastRenewedAt, wantRenewedAt.Format(time.RFC3339))
	}
}

func TestRelayStatusTrackerPeaks(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	tracker := newRelayStatusTracker(startedAt)

	tracker.recordReservationAllowed(false, startedAt.Add(1*time.Minute))
	tracker.recordReservationAllowed(false, startedAt.Add(4*time.Minute))
	tracker.recordReservationClosed(1, startedAt.Add(12*time.Minute))
	tracker.sample(startedAt.Add(21 * time.Minute))

	snapshot := tracker.snapshot(startedAt.Add(25 * time.Minute))
	if got := len(snapshot.Peaks10mLast6h); got != relayStatusPeakBucketCount {
		t.Fatalf("len(Peaks10mLast6h) = %d, want %d", got, relayStatusPeakBucketCount)
	}

	peaksByWindowStart := map[time.Time]int{}
	for _, item := range snapshot.Peaks10mLast6h {
		peaksByWindowStart[item.WindowStart] = item.PeakReservations
	}
	if got := peaksByWindowStart[startedAt]; got != 2 {
		t.Fatalf("peak(%s) = %d, want 2", startedAt.Format(time.RFC3339), got)
	}
	if got := peaksByWindowStart[startedAt.Add(10*time.Minute)]; got != 1 {
		t.Fatalf("peak(%s) = %d, want 1", startedAt.Add(10*time.Minute).Format(time.RFC3339), got)
	}
	if got := peaksByWindowStart[startedAt.Add(20*time.Minute)]; got != 1 {
		t.Fatalf("peak(%s) = %d, want 1", startedAt.Add(20*time.Minute).Format(time.RFC3339), got)
	}
}

func TestRelayStatusPeaksFileRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, relayStatusPeaksFileBaseName)
	startedAt := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Minute)

	source := newRelayStatusTracker(startedAt)
	source.recordReservationAllowed(false, startedAt.Add(1*time.Minute))
	source.recordReservationAllowed(false, startedAt.Add(4*time.Minute))
	source.recordReservationClosed(1, startedAt.Add(12*time.Minute))

	if err := saveRelayStatusPeaks(path, source, now); err != nil {
		t.Fatalf("saveRelayStatusPeaks() error = %v", err)
	}

	target := newRelayStatusTracker(startedAt.Add(2 * time.Hour))
	if err := loadRelayStatusPeaks(path, target, now); err != nil {
		t.Fatalf("loadRelayStatusPeaks() error = %v", err)
	}

	snapshot := target.snapshot(now)
	peaksByWindowStart := map[time.Time]int{}
	for _, item := range snapshot.Peaks10mLast6h {
		peaksByWindowStart[item.WindowStart] = item.PeakReservations
	}
	if got := peaksByWindowStart[startedAt]; got != 2 {
		t.Fatalf("peak(%s) = %d, want 2", startedAt.Format(time.RFC3339), got)
	}
	if got := peaksByWindowStart[startedAt.Add(10*time.Minute)]; got != 1 {
		t.Fatalf("peak(%s) = %d, want 1", startedAt.Add(10*time.Minute).Format(time.RFC3339), got)
	}
}

func TestRelayStatusPeaksFileLoadPrunesOlderThan6Hours(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, relayStatusPeaksFileBaseName)
	now := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	validWindowStart := relayStatusBucketStart(now.Add(-40 * time.Minute))

	payload := relayStatusPeaksFile{
		UpdatedAt: now.Add(-time.Minute),
		Peaks: []relayStatusPeakFileRow{
			{
				WindowStart:      relayStatusBucketStart(now.Add(-8 * time.Hour)),
				PeakReservations: 9,
			},
			{
				WindowStart:      validWindowStart,
				PeakReservations: 3,
			},
		},
	}
	if err := fsstore.WriteJSONAtomic(path, payload, fsstore.FileOptions{}); err != nil {
		t.Fatalf("WriteJSONAtomic() error = %v", err)
	}

	tracker := newRelayStatusTracker(now.Add(-time.Hour))
	if err := loadRelayStatusPeaks(path, tracker, now); err != nil {
		t.Fatalf("loadRelayStatusPeaks() error = %v", err)
	}

	snapshot := tracker.snapshot(now)
	nonZero := 0
	for _, item := range snapshot.Peaks10mLast6h {
		if item.PeakReservations > 0 {
			nonZero++
		}
	}
	if nonZero != 1 {
		t.Fatalf("nonZero peak windows = %d, want 1", nonZero)
	}

	peaksByWindowStart := map[time.Time]int{}
	for _, item := range snapshot.Peaks10mLast6h {
		peaksByWindowStart[item.WindowStart] = item.PeakReservations
	}
	if got := peaksByWindowStart[validWindowStart]; got != 3 {
		t.Fatalf("peak(%s) = %d, want 3", validWindowStart.Format(time.RFC3339), got)
	}
}

func TestRelayStatusHTTPHandler(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 2, 25, 8, 0, 0, 0, time.UTC)
	now := startedAt.Add(30 * time.Minute)
	tracker := newRelayStatusTracker(startedAt)
	tracker.recordReservationAllowed(false, startedAt.Add(1*time.Minute))
	tracker.recordReservationAllowed(true, startedAt.Add(20*time.Minute))
	handler := newRelayObserveHTTPHandler(
		tracker,
		relayv2.Resources{
			MaxReservations:      512,
			MaxReservationsPerIP: 4,
		},
		func() time.Time { return now },
	)

	t.Run("health", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/_hc", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var view relayHealthView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decode /_hc response: %v", err)
		}
		if view.Status != "ok" {
			t.Fatalf("status = %q, want %q", view.Status, "ok")
		}
		if view.Service != "relay" {
			t.Fatalf("service = %q, want %q", view.Service, "relay")
		}
		if view.Version != buildVersion {
			t.Fatalf("version = %q, want %q", view.Version, buildVersion)
		}
	})

	t.Run("status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		var view relayStatusView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decode /status response: %v", err)
		}
		if view.Status != "ok" {
			t.Fatalf("status = %q, want %q", view.Status, "ok")
		}
		if view.UptimeSec != 1800 {
			t.Fatalf("uptime_sec = %d, want 1800", view.UptimeSec)
		}
		if view.MaxReservations != 512 {
			t.Fatalf("max_reservations = %d, want 512", view.MaxReservations)
		}
		if view.MaxReservationsPerIP != 4 {
			t.Fatalf("max_reservations_per_ip = %d, want 4", view.MaxReservationsPerIP)
		}
		if view.Renewal.ActiveReservations != 1 {
			t.Fatalf("active_reservations = %d, want 1", view.Renewal.ActiveReservations)
		}
		if view.Renewal.OpenedTotal != 1 {
			t.Fatalf("opened_total = %d, want 1", view.Renewal.OpenedTotal)
		}
		if view.Renewal.RenewedTotal != 1 {
			t.Fatalf("renewed_total = %d, want 1", view.Renewal.RenewedTotal)
		}
		if got := len(view.Peaks10mLast6h); got != relayStatusPeakBucketCount {
			t.Fatalf("len(peaks_10m_last_6h) = %d, want %d", got, relayStatusPeakBucketCount)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestRelayAdminHTTPHandler(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 25, 8, 30, 0, 0, time.UTC)
	peerIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 25, 8, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	expiresAt := now.Add(20 * time.Minute)
	handler := newRelayAdminHTTPHandler(
		func(_ time.Time) []relayPeerView {
			return []relayPeerView{
				{
					PeerID:    peerIdentity.PeerID,
					ExpiresAt: &expiresAt,
				},
			}
		},
		func() time.Time { return now },
	)

	t.Run("peers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/peers", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var view relayPeersAPIView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decode /peers response: %v", err)
		}
		if len(view.Peers) != 1 {
			t.Fatalf("peer count = %d, want 1", len(view.Peers))
		}
		if view.Peers[0].PeerID != peerIdentity.PeerID {
			t.Fatalf("peer_id = %q, want %q", view.Peers[0].PeerID, peerIdentity.PeerID)
		}
		if view.Peers[0].ExpiresAt == nil || !view.Peers[0].ExpiresAt.Equal(expiresAt) {
			t.Fatalf("expires_at = %v, want %s", view.Peers[0].ExpiresAt, expiresAt.Format(time.RFC3339))
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/peers", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestRelayPeerHasReservationTag(t *testing.T) {
	t.Parallel()

	peerIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 25, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	pid, err := peer.Decode(peerIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode() error = %v", err)
	}

	cm := &relayTestConnManager{
		tagInfoByPeerID: map[string]*connmgr.TagInfo{
			peerIdentity.PeerID: {
				Tags: map[string]int{"relay-reservation": 10},
			},
		},
	}
	if !relayPeerHasReservationTag(cm, pid) {
		t.Fatalf("expected relayPeerHasReservationTag() to return true")
	}

	cm.tagInfoByPeerID[peerIdentity.PeerID] = &connmgr.TagInfo{Tags: map[string]int{"other-tag": 1}}
	if relayPeerHasReservationTag(cm, pid) {
		t.Fatalf("expected relayPeerHasReservationTag() to return false without relay tag")
	}
}

func TestRelayStatusTrackerConnectedPeerViewsOnlyActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	tracker := newRelayStatusTracker(now.Add(-time.Hour))

	activeIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 25, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(active) error = %v", err)
	}
	staleIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 25, 11, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(stale) error = %v", err)
	}
	noLeaseIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 25, 11, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(no-lease) error = %v", err)
	}

	activePID, err := peer.Decode(activeIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(active) error = %v", err)
	}
	stalePID, err := peer.Decode(staleIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(stale) error = %v", err)
	}
	noLeasePID, err := peer.Decode(noLeaseIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(no-lease) error = %v", err)
	}

	ttl := 30 * time.Minute
	tracker.recordPeerLeaseSeen(activeIdentity.PeerID, now.Add(-5*time.Minute), ttl)
	tracker.recordPeerLeaseSeen(staleIdentity.PeerID, now.Add(-50*time.Minute), ttl)

	cm := &relayTestConnManager{
		tagInfoByPeerID: map[string]*connmgr.TagInfo{
			activeIdentity.PeerID: {
				Tags: map[string]int{"relay-reservation": 10},
			},
			staleIdentity.PeerID: {
				Tags: map[string]int{"relay-reservation": 10},
			},
		},
	}
	views := tracker.connectedPeerViews(now, []peer.ID{activePID, stalePID, noLeasePID}, cm)
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1", len(views))
	}
	item := views[0]
	if item.PeerID != activeIdentity.PeerID {
		t.Fatalf("peer_id = %q, want %q", item.PeerID, activeIdentity.PeerID)
	}
	if item.ExpiresAt == nil || !item.ExpiresAt.After(now) {
		t.Fatalf("expires_at = %v, want > now", item.ExpiresAt)
	}
}

type relayTestConnManager struct {
	tagInfoByPeerID map[string]*connmgr.TagInfo
}

func (m *relayTestConnManager) TagPeer(peer.ID, string, int) {}

func (m *relayTestConnManager) UntagPeer(peer.ID, string) {}

func (m *relayTestConnManager) UpsertTag(peer.ID, string, func(int) int) {}

func (m *relayTestConnManager) GetTagInfo(id peer.ID) *connmgr.TagInfo {
	if m == nil {
		return nil
	}
	return m.tagInfoByPeerID[id.String()]
}

func (m *relayTestConnManager) TrimOpenConns(context.Context) {}

func (m *relayTestConnManager) Notifee() network.Notifiee { return network.GlobalNoopNotifiee }

func (m *relayTestConnManager) Protect(peer.ID, string) {}

func (m *relayTestConnManager) Unprotect(peer.ID, string) bool { return false }

func (m *relayTestConnManager) IsProtected(peer.ID, string) bool { return false }

func (m *relayTestConnManager) CheckLimit(connmgr.GetConnLimiter) error { return nil }

func (m *relayTestConnManager) Close() error { return nil }

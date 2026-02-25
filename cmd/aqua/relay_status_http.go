package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/peer"
	pbv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/pb"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

const (
	relayStatusPeakWindow       = 10 * time.Minute
	relayStatusPeakBucketCount  = 36
	relayStatusSamplingInterval = time.Minute

	relayObserveHTTPListenDefault = "127.0.0.1:9632"
	relayAdminSocketBaseName      = "relay-admin.sock"
)

type relayStatusMetricsTracer struct {
	tracker *relayStatusTracker
}

var _ relayv2.MetricsTracer = (*relayStatusMetricsTracer)(nil)

func (t *relayStatusMetricsTracer) RelayStatus(_ bool) {}

func (t *relayStatusMetricsTracer) ConnectionOpened() {}

func (t *relayStatusMetricsTracer) ConnectionClosed(_ time.Duration) {}

func (t *relayStatusMetricsTracer) ConnectionRequestHandled(_ pbv2.Status) {}

func (t *relayStatusMetricsTracer) ReservationAllowed(isRenewal bool) {
	if t == nil || t.tracker == nil {
		return
	}
	t.tracker.recordReservationAllowed(isRenewal, time.Now().UTC())
}

func (t *relayStatusMetricsTracer) ReservationClosed(cnt int) {
	if t == nil || t.tracker == nil {
		return
	}
	t.tracker.recordReservationClosed(cnt, time.Now().UTC())
}

func (t *relayStatusMetricsTracer) ReservationRequestHandled(_ pbv2.Status) {}

func (t *relayStatusMetricsTracer) BytesTransferred(_ int) {}

type relayStatusTracker struct {
	mu sync.RWMutex

	startedAt          time.Time
	activeReservations int
	openedTotal        uint64
	renewedTotal       uint64
	closedTotal        uint64
	lastRenewedAt      *time.Time
	peakByBucket       map[time.Time]int
	peerLeaseExpiresAt map[string]time.Time
}

type relayStatusRenewalView struct {
	ActiveReservations int        `json:"active_reservations"`
	OpenedTotal        uint64     `json:"opened_total"`
	RenewedTotal       uint64     `json:"renewed_total"`
	ClosedTotal        uint64     `json:"closed_total"`
	LastRenewedAt      *time.Time `json:"last_renewed_at"`
}

type relayStatusPeakWindowView struct {
	WindowStart      time.Time `json:"window_start"`
	WindowEnd        time.Time `json:"window_end"`
	PeakReservations int       `json:"peak_reservations"`
}

type relayStatusSnapshot struct {
	StartedAt      time.Time                   `json:"started_at"`
	Now            time.Time                   `json:"now"`
	UptimeSec      int64                       `json:"uptime_sec"`
	Renewal        relayStatusRenewalView      `json:"renewal"`
	Peaks10mLast6h []relayStatusPeakWindowView `json:"peaks_10m_last_6h"`
}

type relayHealthView struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type relayStatusView struct {
	Status               string                      `json:"status"`
	Service              string                      `json:"service"`
	Version              string                      `json:"version"`
	Commit               string                      `json:"commit"`
	Date                 string                      `json:"date"`
	StartedAt            time.Time                   `json:"started_at"`
	Now                  time.Time                   `json:"now"`
	UptimeSec            int64                       `json:"uptime_sec"`
	MaxReservations      int                         `json:"max_reservations"`
	MaxReservationsPerIP int                         `json:"max_reservations_per_ip"`
	Renewal              relayStatusRenewalView      `json:"renewal"`
	Peaks10mLast6h       []relayStatusPeakWindowView `json:"peaks_10m_last_6h"`
}

type relayPeerView struct {
	PeerID    string     `json:"peer_id"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type relayPeersAPIView struct {
	Now   time.Time       `json:"now"`
	Peers []relayPeerView `json:"peers"`
}

func newRelayStatusTracker(startedAt time.Time) *relayStatusTracker {
	startedAt = canonicalRelayStatusTime(startedAt)
	if startedAt.IsZero() {
		startedAt = canonicalRelayStatusTime(time.Now().UTC())
	}
	return &relayStatusTracker{
		startedAt:          startedAt,
		peakByBucket:       make(map[time.Time]int, relayStatusPeakBucketCount),
		peerLeaseExpiresAt: make(map[string]time.Time),
	}
}

func (t *relayStatusTracker) recordReservationAllowed(isRenewal bool, now time.Time) {
	if t == nil {
		return
	}

	now = canonicalRelayStatusTime(now)
	t.mu.Lock()
	defer t.mu.Unlock()

	if isRenewal {
		t.renewedTotal++
		renewedAt := now
		t.lastRenewedAt = &renewedAt
	} else {
		t.openedTotal++
		t.activeReservations++
	}
	t.observeActiveLocked(now)
	t.prunePeaksLocked(now)
}

func (t *relayStatusTracker) recordReservationClosed(cnt int, now time.Time) {
	if t == nil || cnt <= 0 {
		return
	}

	now = canonicalRelayStatusTime(now)
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closedTotal += uint64(cnt)
	t.activeReservations -= cnt
	if t.activeReservations < 0 {
		t.activeReservations = 0
	}
	t.observeActiveLocked(now)
	t.prunePeaksLocked(now)
}

func (t *relayStatusTracker) sample(now time.Time) {
	if t == nil {
		return
	}

	now = canonicalRelayStatusTime(now)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.observeActiveLocked(now)
	t.prunePeaksLocked(now)
}

func (t *relayStatusTracker) snapshot(now time.Time) relayStatusSnapshot {
	now = canonicalRelayStatusTime(now)
	if t == nil {
		return emptyRelayStatusSnapshot(now)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	startedAt := t.startedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	uptime := now.Sub(startedAt)
	if uptime < 0 {
		uptime = 0
	}

	renewal := relayStatusRenewalView{
		ActiveReservations: t.activeReservations,
		OpenedTotal:        t.openedTotal,
		RenewedTotal:       t.renewedTotal,
		ClosedTotal:        t.closedTotal,
	}
	if t.lastRenewedAt != nil {
		last := canonicalRelayStatusTime(*t.lastRenewedAt)
		renewal.LastRenewedAt = &last
	}

	latestStart := relayStatusBucketStart(now)
	oldestStart := latestStart.Add(-time.Duration(relayStatusPeakBucketCount-1) * relayStatusPeakWindow)
	peaks := make([]relayStatusPeakWindowView, 0, relayStatusPeakBucketCount)
	for i := 0; i < relayStatusPeakBucketCount; i++ {
		windowStart := oldestStart.Add(time.Duration(i) * relayStatusPeakWindow)
		peaks = append(peaks, relayStatusPeakWindowView{
			WindowStart:      windowStart,
			WindowEnd:        windowStart.Add(relayStatusPeakWindow),
			PeakReservations: t.peakByBucket[windowStart],
		})
	}

	return relayStatusSnapshot{
		StartedAt:      startedAt,
		Now:            now,
		UptimeSec:      int64(uptime / time.Second),
		Renewal:        renewal,
		Peaks10mLast6h: peaks,
	}
}

func (t *relayStatusTracker) observeActiveLocked(now time.Time) {
	if t.activeReservations < 0 {
		t.activeReservations = 0
	}
	bucketStart := relayStatusBucketStart(now)
	if current, ok := t.peakByBucket[bucketStart]; !ok || t.activeReservations > current {
		t.peakByBucket[bucketStart] = t.activeReservations
	}
}

func (t *relayStatusTracker) prunePeaksLocked(now time.Time) {
	latestStart := relayStatusBucketStart(now)
	oldestStart := latestStart.Add(-time.Duration(relayStatusPeakBucketCount-1) * relayStatusPeakWindow)
	for bucketStart := range t.peakByBucket {
		if bucketStart.Before(oldestStart) || bucketStart.After(latestStart) {
			delete(t.peakByBucket, bucketStart)
		}
	}
}

func (t *relayStatusTracker) recordPeerLeaseSeen(peerID string, now time.Time, ttl time.Duration) {
	if t == nil || ttl <= 0 {
		return
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return
	}
	now = canonicalRelayStatusTime(now)
	expiresAt := now.Add(ttl)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peerLeaseExpiresAt[peerID] = expiresAt
	t.prunePeerLeasesLocked(now, ttl)
}

func (t *relayStatusTracker) prunePeerLeasesLocked(now time.Time, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	cutoff := now.Add(-ttl)
	for peerID, expiresAt := range t.peerLeaseExpiresAt {
		if expiresAt.Before(cutoff) {
			delete(t.peerLeaseExpiresAt, peerID)
		}
	}
}

func (t *relayStatusTracker) connectedPeerViews(
	now time.Time,
	peers []peer.ID,
	cm connmgr.ConnManager,
) []relayPeerView {
	now = canonicalRelayStatusTime(now)
	if len(peers) == 0 {
		return nil
	}

	expiryByPeer := map[string]time.Time{}
	if t != nil {
		t.mu.RLock()
		expiryByPeer = make(map[string]time.Time, len(t.peerLeaseExpiresAt))
		for id, expiresAt := range t.peerLeaseExpiresAt {
			expiryByPeer[id] = expiresAt
		}
		t.mu.RUnlock()
	}

	peerIDs := make([]string, 0, len(peers))
	seen := map[string]bool{}
	for _, pid := range peers {
		id := strings.TrimSpace(pid.String())
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		peerIDs = append(peerIDs, id)
	}
	sort.Strings(peerIDs)

	out := make([]relayPeerView, 0, len(peerIDs))
	for _, id := range peerIDs {
		pid, err := peer.Decode(id)
		if err != nil {
			continue
		}
		if !relayPeerHasReservationTag(cm, pid) {
			continue
		}
		item := relayPeerView{
			PeerID: id,
		}
		if expiresAt, ok := expiryByPeer[id]; ok && !expiresAt.IsZero() {
			expiresAt = canonicalRelayStatusTime(expiresAt)
			if !expiresAt.After(now) {
				continue
			}
			item.ExpiresAt = &expiresAt
		}
		out = append(out, item)
	}
	return out
}

func startRelayStatusSampling(ctx context.Context, tracker *relayStatusTracker) {
	if ctx == nil || tracker == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(relayStatusSamplingInterval)
		defer ticker.Stop()
		for {
			select {
			case tickAt := <-ticker.C:
				tracker.sample(tickAt)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func startRelayObserveHTTPServer(
	listenAddr string,
	logger *slog.Logger,
	tracker *relayStatusTracker,
	resources relayv2.Resources,
) (string, func(context.Context) error, error) {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return "", nil, nil
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return "", nil, fmt.Errorf("listen relay status http server on %q: %w", listenAddr, err)
	}

	server := &http.Server{
		Handler:           newRelayObserveHTTPHandler(tracker, resources, nil),
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdown := runRelayHTTPServer(listener, server, logger, "relay observe server")
	return listener.Addr().String(), shutdown, nil
}

func startRelayAdminHTTPServer(
	socketPath string,
	logger *slog.Logger,
	peerViewsFn func(time.Time) []relayPeerView,
) (string, func(context.Context) error, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return "", nil, nil
	}

	listener, err := listenRelayAdminUnixSocket(socketPath)
	if err != nil {
		return "", nil, err
	}

	server := &http.Server{
		Handler:           newRelayAdminHTTPHandler(peerViewsFn, nil),
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdown := runRelayHTTPServer(listener, server, logger, "relay admin server")
	shutdown = withRelayShutdownCleanup(shutdown, func() error {
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
	return socketPath, shutdown, nil
}

func runRelayHTTPServer(
	listener net.Listener,
	server *http.Server,
	logger *slog.Logger,
	name string,
) func(context.Context) error {
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if logger != nil {
				logger.Error(name+" stopped with error", "error", err)
			}
		}
	}()
	return func(ctx context.Context) error {
		err := server.Shutdown(ctx)
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && err == nil {
			err = closeErr
		}
		return err
	}
}

func withRelayShutdownCleanup(
	shutdown func(context.Context) error,
	cleanup func() error,
) func(context.Context) error {
	return func(ctx context.Context) error {
		err := shutdown(ctx)
		if cleanup != nil {
			if cleanupErr := cleanup(); cleanupErr != nil && err == nil {
				err = cleanupErr
			}
		}
		return err
	}
}

func listenRelayAdminUnixSocket(socketPath string) (net.Listener, error) {
	dir := strings.TrimSpace(filepath.Dir(socketPath))
	if dir == "" {
		return nil, fmt.Errorf("invalid relay admin socket path %q", socketPath)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create relay admin socket dir %q: %w", dir, err)
	}

	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("relay admin socket path exists and is not a socket: %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale relay admin socket %q: %w", socketPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat relay admin socket %q: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen relay admin socket %q: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("chmod relay admin socket %q: %w", socketPath, err)
	}
	return listener, nil
}

func newRelayObserveHTTPHandler(
	tracker *relayStatusTracker,
	resources relayv2.Resources,
	nowFn func() time.Time,
) http.Handler {
	if nowFn == nil {
		nowFn = func() time.Time {
			return time.Now().UTC()
		}
	}
	if tracker == nil {
		tracker = newRelayStatusTracker(nowFn())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_hc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRelayStatusError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeRelayStatusJSON(w, http.StatusOK, relayHealthView{
			Status:  "ok",
			Service: "relay",
			Version: buildVersion,
			Commit:  buildCommit,
			Date:    buildDate,
		})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRelayStatusError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		now := nowFn()
		snapshot := tracker.snapshot(now)
		writeRelayStatusJSON(w, http.StatusOK, relayStatusView{
			Status:               "ok",
			Service:              "relay",
			Version:              buildVersion,
			Commit:               buildCommit,
			Date:                 buildDate,
			StartedAt:            snapshot.StartedAt,
			Now:                  snapshot.Now,
			UptimeSec:            snapshot.UptimeSec,
			MaxReservations:      resources.MaxReservations,
			MaxReservationsPerIP: resources.MaxReservationsPerIP,
			Renewal:              snapshot.Renewal,
			Peaks10mLast6h:       snapshot.Peaks10mLast6h,
		})
	})
	return mux
}

func newRelayAdminHTTPHandler(peerViewsFn func(time.Time) []relayPeerView, nowFn func() time.Time) http.Handler {
	if nowFn == nil {
		nowFn = func() time.Time {
			return time.Now().UTC()
		}
	}
	if peerViewsFn == nil {
		peerViewsFn = func(time.Time) []relayPeerView { return nil }
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRelayStatusError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		now := canonicalRelayStatusTime(nowFn())
		writeRelayStatusJSON(w, http.StatusOK, relayPeersAPIView{
			Now:   now,
			Peers: peerViewsFn(now),
		})
	})
	return mux
}

func writeRelayStatusJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = writeJSON(w, payload)
}

func writeRelayStatusError(w http.ResponseWriter, statusCode int, message string) {
	writeRelayStatusJSON(w, statusCode, map[string]string{
		"status": "error",
		"error":  strings.TrimSpace(message),
	})
}

func emptyRelayStatusSnapshot(now time.Time) relayStatusSnapshot {
	now = canonicalRelayStatusTime(now)
	latestStart := relayStatusBucketStart(now)
	oldestStart := latestStart.Add(-time.Duration(relayStatusPeakBucketCount-1) * relayStatusPeakWindow)
	peaks := make([]relayStatusPeakWindowView, 0, relayStatusPeakBucketCount)
	for i := 0; i < relayStatusPeakBucketCount; i++ {
		windowStart := oldestStart.Add(time.Duration(i) * relayStatusPeakWindow)
		peaks = append(peaks, relayStatusPeakWindowView{
			WindowStart:      windowStart,
			WindowEnd:        windowStart.Add(relayStatusPeakWindow),
			PeakReservations: 0,
		})
	}
	return relayStatusSnapshot{
		StartedAt:      now,
		Now:            now,
		UptimeSec:      0,
		Renewal:        relayStatusRenewalView{},
		Peaks10mLast6h: peaks,
	}
}

func canonicalRelayStatusTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Round(0)
}

func relayStatusBucketStart(t time.Time) time.Time {
	canonical := canonicalRelayStatusTime(t)
	if canonical.IsZero() {
		return time.Time{}
	}
	return canonical.Truncate(relayStatusPeakWindow)
}

func relayPeerHasReservationTag(cm connmgr.ConnManager, pid peer.ID) bool {
	if cm == nil {
		return false
	}
	tagInfo := cm.GetTagInfo(pid)
	if tagInfo == nil {
		return false
	}
	value, ok := tagInfo.Tags["relay-reservation"]
	return ok && value > 0
}

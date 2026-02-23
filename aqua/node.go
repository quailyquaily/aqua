package aqua

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	libp2p "github.com/libp2p/go-libp2p"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	sessionFreshWindow        = 10 * time.Minute
	incomingQueueSize         = 4096
	incomingEnqueueTimeout    = 20 * time.Millisecond
	incomingFlushMaxBatch     = 256
	incomingFlushInterval     = 200 * time.Millisecond
	incomingDedupePrunePeriod = time.Second
	storeWriteTimeout         = 2 * time.Second
)

type NodeOptions struct {
	DialOnly           bool
	ListenAddrs        []string
	RelayAddrs         []string
	RelayMode          string
	DialAddrTimeout    time.Duration
	HelloTimeout       time.Duration
	RPCTimeout         time.Duration
	MaxRPCRequestBytes int
	MaxPayloadBytes    int
	DataPushPerMinute  int
	DedupeTTL          time.Duration
	DedupeMaxEntries   int
	Logger             *slog.Logger
	OnDataPush         func(event DataPushEvent)
	OnRelayEvent       func(event RelayEvent)
}

type HelloResult struct {
	RemotePeerID       string
	RemoteMinProtocol  int
	RemoteMaxProtocol  int
	NegotiatedProtocol int
	UpdatedAt          time.Time
}

type Node struct {
	host  host.Host
	svc   *Service
	store Store
	local Identity
	opts  NodeOptions

	mu       sync.RWMutex
	sessions map[string]HelloResult

	rateMu          sync.Mutex
	pushRateWindows map[string]pushRateWindow

	incomingQueue     chan incomingPushEnvelope
	incomingCloseOnce sync.Once
	incomingWG        sync.WaitGroup

	dedupeMu            sync.Mutex
	incomingDedupeCache map[string]time.Time

	relayMu             sync.RWMutex
	relayAdvertiseAddrs []string
}

type pushRateWindow struct {
	WindowMinute time.Time
	Count        int
}

type incomingPushEnvelope struct {
	message InboxMessage
}

type helloMessage struct {
	Type         string   `json:"type"`
	ProtocolMin  int      `json:"protocol_min"`
	ProtocolMax  int      `json:"protocol_max"`
	Capabilities []string `json:"capabilities"`
}

func NewNode(ctx context.Context, svc *Service, opts NodeOptions) (*Node, error) {
	if svc == nil || svc.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	identity, ok, err := svc.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("identity not found; run `aqua init`")
	}
	priv, err := ParseIdentityPrivateKey(identity.IdentityPrivEd25519)
	if err != nil {
		return nil, err
	}
	explicitListenProvided := len(normalizeAddresses(opts.ListenAddrs)) > 0

	options, err := normalizeNodeOptions(opts)
	if err != nil {
		return nil, err
	}

	h, err := newLibp2pHost(priv, options.DialOnly, options.ListenAddrs)
	if err != nil {
		if !options.DialOnly && !explicitListenProvided {
			fallbackListenAddrs := defaultFallbackListenAddrs()
			fallbackHost, fallbackErr := newLibp2pHost(priv, false, fallbackListenAddrs)
			if fallbackErr == nil {
				h = fallbackHost
				options.ListenAddrs = fallbackListenAddrs
			} else {
				return nil, fmt.Errorf("create libp2p host: default listen failed (%v); fallback listen failed (%w)", err, fallbackErr)
			}
		} else {
			return nil, fmt.Errorf("create libp2p host: %w", err)
		}
	}

	if h.ID().String() != identity.PeerID {
		_ = h.Close()
		return nil, fmt.Errorf("libp2p host identity mismatch: host=%s identity=%s", h.ID().String(), identity.PeerID)
	}

	n := &Node{
		host:                h,
		svc:                 svc,
		store:               svc.store,
		local:               identity,
		opts:                options,
		sessions:            map[string]HelloResult{},
		pushRateWindows:     map[string]pushRateWindow{},
		incomingQueue:       make(chan incomingPushEnvelope, incomingQueueSize),
		incomingDedupeCache: map[string]time.Time{},
	}

	h.SetStreamHandler(protocol.ID(ProtocolHelloIDV1), n.handleHelloStream)
	h.SetStreamHandler(protocol.ID(ProtocolRPCIDV1), n.handleRPCStream)
	n.startIncomingWriter()

	if !options.DialOnly && options.RelayMode != RelayModeOff && len(options.RelayAddrs) > 0 {
		if err := n.reserveConfiguredRelays(ctx); err != nil {
			if options.RelayMode == RelayModeRequired {
				_ = h.Close()
				return nil, fmt.Errorf("reserve relays: %w", err)
			}
			n.opts.Logger.Warn("relay reservation unavailable", "err", err)
		}
	}

	return n, nil
}

func (n *Node) Close() error {
	if n == nil {
		return nil
	}
	var closeErr error
	if n.host != nil {
		closeErr = n.host.Close()
	}
	n.stopIncomingWriter()
	return closeErr
}

func (n *Node) PeerID() string {
	if n == nil || n.host == nil {
		return ""
	}
	return n.host.ID().String()
}

func (n *Node) AddrStrings() []string {
	if n == nil || n.host == nil {
		return nil
	}
	baseAddrs := n.host.Addrs()
	baseStrings := make([]string, 0, len(baseAddrs))
	for _, addr := range baseAddrs {
		baseStrings = append(baseStrings, addr.String())
	}
	baseStrings = append(baseStrings, n.relayAdvertiseAddrsSnapshot()...)
	advertiseStrings := normalizeAddresses(baseStrings)
	if hasWildcardListenAddress(n.opts.ListenAddrs) {
		if localIPs, err := discoverInterfaceIPs(); err == nil && len(localIPs) > 0 {
			advertiseStrings = expandAdvertiseAddresses(advertiseStrings, localIPs)
		}
	}

	out := make([]string, 0, len(advertiseStrings))
	p2pComponent, err := ma.NewMultiaddr("/p2p/" + n.host.ID().String())
	if err != nil {
		return nil
	}
	for _, rawAddr := range advertiseStrings {
		addr, err := ma.NewMultiaddr(rawAddr)
		if err != nil {
			continue
		}
		out = append(out, addr.Encapsulate(p2pComponent).String())
	}
	out = normalizeAddresses(out)
	sortStrings(out)
	return out
}

func hasWildcardListenAddress(listenAddrs []string) bool {
	for _, raw := range normalizeAddresses(listenAddrs) {
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

func expandAdvertiseAddresses(baseAddrs []string, localIPs []net.IP) []string {
	out := make([]string, 0, len(baseAddrs))
	seen := map[string]bool{}
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}

	for _, raw := range normalizeAddresses(baseAddrs) {
		add(raw)
		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}

		if _, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
			for _, localIP := range localIPs {
				v4 := localIP.To4()
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
			for _, localIP := range localIPs {
				if localIP.To4() != nil || localIP.To16() == nil {
					continue
				}
				replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP6, localIP.String())
				if err != nil {
					continue
				}
				add(replaced.String())
			}
		}
	}
	return out
}

func (n *Node) Ping(ctx context.Context, peerID string, addresses []string) (map[string]any, error) {
	resultRaw, err := n.callRPC(ctx, peerID, addresses, "agent.ping", map[string]any{}, false)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeRPCJSON(resultRaw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (n *Node) GetCapabilities(ctx context.Context, peerID string, addresses []string) (map[string]any, error) {
	resultRaw, err := n.callRPC(ctx, peerID, addresses, "agent.capabilities.get", map[string]any{}, false)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeRPCJSON(resultRaw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (n *Node) GetContactCard(ctx context.Context, peerID string, addresses []string) ([]byte, error) {
	expectedPeerID, dialAddresses, err := resolveExplicitDialTarget(peerID, addresses)
	if err != nil {
		return nil, err
	}
	resultRaw, err := n.callRPCResolved(
		ctx,
		expectedPeerID,
		dialAddresses,
		"agent.card.get",
		rpcCardGetParams{Addresses: dialAddresses},
		false,
		false,
	)
	if err != nil {
		return nil, err
	}
	var result rpcCardGetResult
	if err := decodeRPCJSON(resultRaw, &result); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(result.ContactCardJSON)
	if trimmed == "" {
		return nil, WrapProtocolError(ErrInvalidParams, "empty contact_card_json")
	}
	return []byte(trimmed), nil
}

func (n *Node) PushData(ctx context.Context, peerID string, addresses []string, req DataPushRequest, notification bool) (DataPushResult, error) {
	req.Topic = strings.TrimSpace(req.Topic)
	req.ContentType = strings.TrimSpace(req.ContentType)
	req.PayloadBase64 = strings.TrimSpace(req.PayloadBase64)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.ReplyTo = strings.TrimSpace(req.ReplyTo)

	payloadBytes, decodeErr := base64.RawURLEncoding.DecodeString(req.PayloadBase64)
	if decodeErr != nil {
		return DataPushResult{}, WrapProtocolError(ErrInvalidParams, "payload_base64 decode failed")
	}
	if len(payloadBytes) > n.opts.MaxPayloadBytes {
		return DataPushResult{}, WrapProtocolError(ErrPayloadTooLarge, "payload exceeds max_payload_bytes")
	}
	if _, _, err := validateSessionForTopic(req.Topic, req.SessionID, req.ReplyTo); err != nil {
		return DataPushResult{}, WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}

	params := map[string]any{
		"topic":           req.Topic,
		"content_type":    req.ContentType,
		"payload_base64":  req.PayloadBase64,
		"idempotency_key": req.IdempotencyKey,
	}
	if req.SessionID != "" {
		params["session_id"] = req.SessionID
	}
	if req.ReplyTo != "" {
		params["reply_to"] = req.ReplyTo
	}
	resultRaw, err := n.callRPC(ctx, peerID, addresses, "agent.data.push", params, notification)
	if err != nil {
		return DataPushResult{}, err
	}
	result := DataPushResult{Accepted: true, Deduped: false}
	if !notification {
		if err := decodeRPCJSON(resultRaw, &result); err != nil {
			return DataPushResult{}, err
		}
	}

	now := time.Now().UTC()
	outboxMessage := OutboxMessage{
		MessageID:      "msg_" + uuid.NewString(),
		ToPeerID:       strings.TrimSpace(peerID),
		Topic:          req.Topic,
		ContentType:    req.ContentType,
		PayloadBase64:  req.PayloadBase64,
		IdempotencyKey: req.IdempotencyKey,
		SessionID:      req.SessionID,
		ReplyTo:        req.ReplyTo,
		SentAt:         now,
	}
	storeCtx, cancelStore := n.storeContext(context.Background())
	defer cancelStore()
	if err := n.store.AppendOutboxMessage(storeCtx, outboxMessage); err != nil {
		n.opts.Logger.Warn("append outbox message failed", "peer_id", peerID, "err", err)
	}

	return result, nil
}

func (n *Node) startIncomingWriter() {
	if n == nil || n.incomingQueue == nil {
		return
	}
	n.incomingWG.Add(1)
	go n.runIncomingWriter()
}

func (n *Node) stopIncomingWriter() {
	if n == nil {
		return
	}
	n.incomingCloseOnce.Do(func() {
		if n.incomingQueue != nil {
			close(n.incomingQueue)
		}
		n.incomingWG.Wait()
	})
}

func (n *Node) runIncomingWriter() {
	defer n.incomingWG.Done()
	if n == nil {
		return
	}
	flushTicker := time.NewTicker(incomingFlushInterval)
	defer flushTicker.Stop()
	dedupeTicker := time.NewTicker(incomingDedupePrunePeriod)
	defer dedupeTicker.Stop()

	batch := make([]incomingPushEnvelope, 0, incomingFlushMaxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		n.flushIncomingBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-n.incomingQueue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= incomingFlushMaxBatch {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case now := <-dedupeTicker.C:
			n.pruneIncomingDedupe(now.UTC())
		}
	}
}

func (n *Node) flushIncomingBatch(batch []incomingPushEnvelope) {
	if len(batch) == 0 {
		return
	}
	messages := make([]InboxMessage, 0, len(batch))
	for _, item := range batch {
		messages = append(messages, item.message)
	}
	storeCtx, cancelStore := n.storeContext(context.Background())
	defer cancelStore()
	if err := n.store.AppendInboxMessages(storeCtx, messages); err != nil {
		n.opts.Logger.Warn("append inbox batch failed", "size", len(messages), "err", err)
	}
}

func (n *Node) enqueueIncomingPush(item incomingPushEnvelope) (err error) {
	if n == nil || n.incomingQueue == nil {
		return WrapProtocolError(ErrBusy, "incoming queue unavailable")
	}
	defer func() {
		if recover() != nil {
			err = WrapProtocolError(ErrBusy, "incoming queue unavailable")
		}
	}()
	timer := time.NewTimer(incomingEnqueueTimeout)
	defer timer.Stop()
	select {
	case n.incomingQueue <- item:
		return nil
	case <-timer.C:
		return WrapProtocolError(ErrBusy, "incoming queue is full")
	}
}

func incomingDedupeCacheKey(fromPeerID string, topic string, idempotencyKey string) string {
	return strings.TrimSpace(fromPeerID) + "\x1f" + strings.TrimSpace(topic) + "\x1f" + strings.TrimSpace(idempotencyKey)
}

func (n *Node) reserveIncomingDedupe(fromPeerID string, topic string, idempotencyKey string, now time.Time) (string, bool) {
	if n == nil {
		return "", false
	}
	cacheKey := incomingDedupeCacheKey(fromPeerID, topic, idempotencyKey)
	expiresAt := now.Add(n.opts.DedupeTTL)

	n.dedupeMu.Lock()
	defer n.dedupeMu.Unlock()

	if existingExpiry, ok := n.incomingDedupeCache[cacheKey]; ok && existingExpiry.After(now) {
		return cacheKey, true
	}
	n.incomingDedupeCache[cacheKey] = expiresAt
	return cacheKey, false
}

func (n *Node) releaseIncomingDedupe(cacheKey string) {
	if n == nil || strings.TrimSpace(cacheKey) == "" {
		return
	}
	n.dedupeMu.Lock()
	delete(n.incomingDedupeCache, cacheKey)
	n.dedupeMu.Unlock()
}

func (n *Node) pruneIncomingDedupeLocked(now time.Time) {
	if len(n.incomingDedupeCache) == 0 {
		return
	}
	for key, expiry := range n.incomingDedupeCache {
		if !expiry.After(now) {
			delete(n.incomingDedupeCache, key)
		}
	}
	if len(n.incomingDedupeCache) <= n.opts.DedupeMaxEntries {
		return
	}
	overflow := len(n.incomingDedupeCache) - n.opts.DedupeMaxEntries
	dropKeys := make([]string, 0, overflow)
	for key := range n.incomingDedupeCache {
		dropKeys = append(dropKeys, key)
		if len(dropKeys) >= overflow {
			break
		}
	}
	for _, key := range dropKeys {
		delete(n.incomingDedupeCache, key)
	}
}

func (n *Node) pruneIncomingDedupe(now time.Time) {
	if n == nil {
		return
	}
	n.dedupeMu.Lock()
	n.pruneIncomingDedupeLocked(now)
	n.dedupeMu.Unlock()
}

func (n *Node) storeContext(base context.Context) (context.Context, context.CancelFunc) {
	if base == nil {
		base = context.Background()
	}
	return withTimeoutIfNeeded(base, storeWriteTimeout)
}

func (n *Node) DialHello(ctx context.Context, peerID string, addresses []string) (HelloResult, error) {
	expectedPeerID, dialAddresses, _, err := n.resolveDialTarget(ctx, peerID, addresses)
	if err != nil {
		return HelloResult{}, err
	}
	return n.dialHelloResolved(ctx, expectedPeerID, dialAddresses)
}

func (n *Node) dialHelloResolved(ctx context.Context, expectedPeerID peer.ID, dialAddresses []string) (HelloResult, error) {
	timeoutCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.HelloTimeout)
	defer cancel()

	if err := n.connect(timeoutCtx, expectedPeerID, dialAddresses); err != nil {
		return HelloResult{}, err
	}

	// Relay circuits are often "limited" connections. Allow opening streams on them.
	streamCtx := network.WithAllowLimitedConn(timeoutCtx, "aqua-hello")
	stream, err := n.host.NewStream(streamCtx, expectedPeerID, protocol.ID(ProtocolHelloIDV1))
	if err != nil {
		return HelloResult{}, fmt.Errorf("open hello stream: %w", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().UTC().Add(n.opts.HelloTimeout))

	if err := verifyRemotePeerOnStream(stream, expectedPeerID); err != nil {
		_ = stream.Reset()
		return HelloResult{}, err
	}

	localHello := n.localHelloMessage()
	reqRaw, err := json.Marshal(localHello)
	if err != nil {
		return HelloResult{}, err
	}
	if _, err := stream.Write(reqRaw); err != nil {
		return HelloResult{}, fmt.Errorf("write hello request: %w", err)
	}
	if err := stream.CloseWrite(); err != nil {
		return HelloResult{}, fmt.Errorf("close hello write: %w", err)
	}

	respRaw, tooLarge, err := readAllLimited(stream, n.opts.MaxRPCRequestBytes)
	if err != nil {
		return HelloResult{}, fmt.Errorf("read hello response: %w", err)
	}
	if tooLarge {
		return HelloResult{}, WrapProtocolError(ErrPayloadTooLarge, "hello response exceeds limit")
	}

	remoteHello, err := parseHelloMessage(respRaw)
	if err != nil {
		return HelloResult{}, err
	}
	negotiated, err := negotiateProtocol(localHello.ProtocolMin, localHello.ProtocolMax, remoteHello.ProtocolMin, remoteHello.ProtocolMax)
	if err != nil {
		return HelloResult{}, err
	}

	result := HelloResult{
		RemotePeerID:       expectedPeerID.String(),
		RemoteMinProtocol:  remoteHello.ProtocolMin,
		RemoteMaxProtocol:  remoteHello.ProtocolMax,
		NegotiatedProtocol: negotiated,
	}
	if err := n.recordSession(result); err != nil {
		n.opts.Logger.Warn("record hello session failed", "peer_id", expectedPeerID.String(), "err", err)
	}
	return result, nil
}

func (n *Node) callRPC(ctx context.Context, peerID string, addresses []string, method string, params any, notification bool) (json.RawMessage, error) {
	expectedPeerID, dialAddresses, _, err := n.resolveDialTarget(ctx, peerID, addresses)
	if err != nil {
		return nil, err
	}
	return n.callRPCResolved(ctx, expectedPeerID, dialAddresses, method, params, notification, false)
}

func (n *Node) callRPCResolved(ctx context.Context, expectedPeerID peer.ID, dialAddresses []string, method string, params any, notification bool, retriedUnsupported bool) (json.RawMessage, error) {
	if rpcMethodRequiresSession(method) && !n.hasFreshSession(expectedPeerID.String()) {
		n.opts.Logger.Debug(
			"rpc session missing; negotiating hello",
			"peer_id", expectedPeerID.String(),
			"method", method,
		)
		if _, err := n.dialHelloResolved(ctx, expectedPeerID, dialAddresses); err != nil {
			return nil, err
		}
	}

	timeoutCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.RPCTimeout)
	defer cancel()

	if err := n.connect(timeoutCtx, expectedPeerID, dialAddresses); err != nil {
		return nil, err
	}
	connsAfterDial := n.host.Network().ConnsToPeer(expectedPeerID)
	n.opts.Logger.Debug(
		"rpc connected",
		"peer_id", expectedPeerID.String(),
		"method", method,
		"notification", notification,
		"connectedness", n.host.Network().Connectedness(expectedPeerID).String(),
		"conn_count", len(connsAfterDial),
		"conn_details", connDebugDetails(connsAfterDial),
	)

	// Relay circuits are often "limited" connections. Allow opening streams on them.
	streamCtx := network.WithAllowLimitedConn(timeoutCtx, "aqua-rpc")
	stream, err := n.host.NewStream(streamCtx, expectedPeerID, protocol.ID(ProtocolRPCIDV1))
	if err != nil {
		connsOnFailure := n.host.Network().ConnsToPeer(expectedPeerID)
		n.opts.Logger.Debug(
			"rpc stream open failed",
			"peer_id", expectedPeerID.String(),
			"method", method,
			"err", err,
			"connectedness", n.host.Network().Connectedness(expectedPeerID).String(),
			"conn_count", len(connsOnFailure),
			"conn_details", connDebugDetails(connsOnFailure),
		)
		return nil, fmt.Errorf("open rpc stream: %w", err)
	}
	n.opts.Logger.Debug(
		"rpc stream opened",
		"peer_id", expectedPeerID.String(),
		"method", method,
		"stream_id", stream.ID(),
	)
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().UTC().Add(n.opts.RPCTimeout))

	if err := verifyRemotePeerOnStream(stream, expectedPeerID); err != nil {
		_ = stream.Reset()
		return nil, err
	}

	reqObj := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"method":  method,
		"params":  params,
	}
	var requestID any
	if !notification {
		requestID = generateRPCRequestID()
		reqObj["id"] = requestID
	}
	reqRaw, err := json.Marshal(reqObj)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}
	if len(reqRaw) > n.opts.MaxRPCRequestBytes {
		return nil, WrapProtocolError(ErrPayloadTooLarge, "rpc request exceeds limit")
	}

	if _, err := stream.Write(reqRaw); err != nil {
		return nil, fmt.Errorf("write rpc request: %w", err)
	}
	if err := stream.CloseWrite(); err != nil {
		return nil, fmt.Errorf("close rpc write: %w", err)
	}
	n.opts.Logger.Debug(
		"rpc request sent",
		"peer_id", expectedPeerID.String(),
		"method", method,
		"notification", notification,
		"request_bytes", len(reqRaw),
	)
	if notification {
		return nil, nil
	}

	respRaw, tooLarge, err := readAllLimited(stream, n.opts.MaxRPCRequestBytes)
	if err != nil {
		return nil, fmt.Errorf("read rpc response: %w", err)
	}
	if tooLarge {
		return nil, WrapProtocolError(ErrPayloadTooLarge, "rpc response exceeds limit")
	}

	result, symbol, details, err := parseRPCResponse(respRaw)
	if err != nil {
		return nil, err
	}
	n.opts.Logger.Debug(
		"rpc response received",
		"peer_id", expectedPeerID.String(),
		"method", method,
		"response_bytes", len(respRaw),
		"symbol", strings.TrimSpace(symbol),
	)
	if strings.TrimSpace(symbol) != "" {
		// Handles session drift (for example remote node restart) by renegotiating once.
		if rpcMethodRequiresSession(method) && shouldRetryAfterUnsupported(symbol, retriedUnsupported) {
			n.opts.Logger.Debug(
				"rpc retry after unsupported protocol",
				"peer_id", expectedPeerID.String(),
				"method", method,
			)
			if _, err := n.dialHelloResolved(ctx, expectedPeerID, dialAddresses); err != nil {
				return nil, err
			}
			return n.callRPCResolved(ctx, expectedPeerID, dialAddresses, method, params, notification, true)
		}
		return nil, protocolErrorFromSymbol(symbol, details)
	}
	return result, nil
}

func shouldRetryAfterUnsupported(symbol string, retried bool) bool {
	if retried {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(symbol), ErrUnsupportedProtocolSymbol)
}

func rpcMethodRequiresSession(method string) bool {
	switch strings.TrimSpace(method) {
	case "agent.card.get":
		return false
	default:
		return true
	}
}

func rpcMethodRequiresAuthorizedPeer(method string) bool {
	switch strings.TrimSpace(method) {
	case "agent.card.get":
		return false
	default:
		return true
	}
}

func (n *Node) handleHelloStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().UTC().Add(n.opts.HelloTimeout))

	remotePeer := stream.Conn().RemotePeer().String()
	n.opts.Logger.Debug("hello stream accepted", "peer_id", remotePeer, "stream_id", stream.ID())
	allowedCtx, cancelAllowed := n.storeContext(context.Background())
	_, allowedErr := n.ensurePeerAllowed(allowedCtx, remotePeer)
	cancelAllowed()
	if allowedErr != nil {
		n.opts.Logger.Warn("reject hello from unauthorized peer", "peer_id", remotePeer, "err", allowedErr)
		_ = stream.Conn().Close()
		return
	}

	if err := verifyRemotePeerOnStream(stream, stream.Conn().RemotePeer()); err != nil {
		n.opts.Logger.Warn("reject hello due to peer mismatch", "peer_id", remotePeer, "err", err)
		_ = stream.Conn().Close()
		return
	}

	reqRaw, tooLarge, err := readAllLimited(stream, n.opts.MaxRPCRequestBytes)
	if err != nil {
		n.opts.Logger.Warn("read hello request failed", "peer_id", remotePeer, "err", err)
		return
	}
	if tooLarge {
		n.opts.Logger.Warn("hello request too large", "peer_id", remotePeer)
		_ = stream.Conn().Close()
		return
	}

	remoteHello, err := parseHelloMessage(reqRaw)
	if err != nil {
		n.opts.Logger.Warn("invalid hello request", "peer_id", remotePeer, "err", err)
		_ = stream.Conn().Close()
		return
	}
	n.opts.Logger.Debug(
		"hello request parsed",
		"peer_id", remotePeer,
		"remote_min", remoteHello.ProtocolMin,
		"remote_max", remoteHello.ProtocolMax,
	)

	localHello := n.localHelloMessage()
	negotiated, err := negotiateProtocol(localHello.ProtocolMin, localHello.ProtocolMax, remoteHello.ProtocolMin, remoteHello.ProtocolMax)
	if err != nil {
		n.opts.Logger.Warn("hello negotiation failed", "peer_id", remotePeer, "err", err)
		_ = stream.Conn().Close()
		return
	}

	result := HelloResult{
		RemotePeerID:       remotePeer,
		RemoteMinProtocol:  remoteHello.ProtocolMin,
		RemoteMaxProtocol:  remoteHello.ProtocolMax,
		NegotiatedProtocol: negotiated,
	}
	if err := n.recordSession(result); err != nil {
		n.opts.Logger.Warn("record hello session failed", "peer_id", remotePeer, "err", err)
	}
	n.opts.Logger.Debug("hello negotiation ok", "peer_id", remotePeer, "negotiated", negotiated)

	respRaw, err := json.Marshal(localHello)
	if err != nil {
		n.opts.Logger.Warn("marshal hello response failed", "peer_id", remotePeer, "err", err)
		return
	}
	if _, err := stream.Write(respRaw); err != nil {
		n.opts.Logger.Warn("write hello response failed", "peer_id", remotePeer, "err", err)
	}
}

func (n *Node) handleRPCStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().UTC().Add(n.opts.RPCTimeout))

	remotePeerID := stream.Conn().RemotePeer().String()
	n.opts.Logger.Debug("rpc stream accepted", "peer_id", remotePeerID, "stream_id", stream.ID())
	if err := verifyRemotePeerOnStream(stream, stream.Conn().RemotePeer()); err != nil {
		n.opts.Logger.Warn("reject rpc due to peer mismatch", "peer_id", remotePeerID, "err", err)
		_ = stream.Conn().Close()
		return
	}

	raw, tooLarge, err := readAllLimited(stream, n.opts.MaxRPCRequestBytes)
	if err != nil {
		n.opts.Logger.Warn("read rpc request failed", "peer_id", remotePeerID, "err", err)
		return
	}
	if tooLarge {
		_, _ = n.writeRPCError(stream, nil, ErrPayloadTooLargeSymbol, "request exceeds max_rpc_request_bytes")
		return
	}

	req, err := parseRPCRequest(raw)
	if err != nil {
		n.opts.Logger.Warn("invalid rpc request", "peer_id", remotePeerID, "err", err)
		if reqID, hasID := extractRPCIDForError(raw); hasID {
			symbol := SymbolOf(err)
			if strings.TrimSpace(symbol) == "" {
				symbol = ErrInvalidParamsSymbol
			}
			_, _ = n.writeRPCError(stream, reqID, symbol, err.Error())
		}
		return
	}
	n.opts.Logger.Debug(
		"rpc request parsed",
		"peer_id", remotePeerID,
		"method", req.Method,
		"has_id", req.HasID,
	)

	if !isAllowedMethod(req.Method) {
		if req.HasID {
			_, _ = n.writeRPCError(stream, req.ID, ErrMethodNotAllowedSymbol, "method="+req.Method)
		}
		return
	}

	if rpcMethodRequiresSession(req.Method) && !n.hasFreshSession(remotePeerID) {
		if req.HasID {
			_, _ = n.writeRPCError(stream, req.ID, ErrUnsupportedProtocolSymbol, "hello negotiation required before rpc")
		}
		return
	}

	if rpcMethodRequiresAuthorizedPeer(req.Method) {
		allowedCtx, cancelAllowed := n.storeContext(context.Background())
		_, allowedErr := n.ensurePeerAllowed(allowedCtx, remotePeerID)
		cancelAllowed()
		if allowedErr != nil {
			if req.HasID {
				_, _ = n.writeRPCError(stream, req.ID, ErrUnauthorizedSymbol, allowedErr.Error())
			}
			_ = stream.Conn().Close()
			return
		}
	}

	result, symbol, details := n.handleRPCMethod(remotePeerID, req)
	if !req.HasID {
		if symbol != "" {
			n.opts.Logger.Warn("rpc notification rejected", "peer_id", remotePeerID, "method", req.Method, "symbol", symbol, "details", details)
		}
		n.opts.Logger.Debug("rpc notification handled", "peer_id", remotePeerID, "method", req.Method, "symbol", strings.TrimSpace(symbol))
		return
	}

	if strings.TrimSpace(symbol) != "" {
		_, _ = n.writeRPCError(stream, req.ID, symbol, details)
		n.opts.Logger.Debug("rpc response error", "peer_id", remotePeerID, "method", req.Method, "symbol", symbol)
		return
	}
	_, _ = n.writeRPCSuccess(stream, req.ID, result)
	n.opts.Logger.Debug("rpc response success", "peer_id", remotePeerID, "method", req.Method)
}

func (n *Node) handleRPCMethod(fromPeerID string, req rpcRequest) (any, string, string) {
	switch req.Method {
	case "agent.ping":
		return map[string]any{
			"ok": true,
			"ts": time.Now().UTC().Format(time.RFC3339),
		}, "", ""
	case "agent.capabilities.get":
		return map[string]any{
			"protocol_min":    ProtocolVersionV1,
			"protocol_max":    ProtocolVersionV1,
			"capabilities":    []string{CapabilityDataPushV1},
			"allowed_methods": append([]string(nil), allowedMethodsV1...),
		}, "", ""
	case "agent.card.get":
		var params rpcCardGetParams
		if err := decodeRPCParams(req.Params, &params); err != nil {
			return nil, ErrInvalidParamsSymbol, err.Error()
		}
		addresses := normalizeAddresses(params.Addresses)
		if len(addresses) == 0 {
			return nil, ErrInvalidParamsSymbol, "addresses is required"
		}
		exportCtx, cancelExport := n.storeContext(context.Background())
		_, rawCard, err := n.svc.ExportContactCard(
			exportCtx,
			addresses,
			ProtocolVersionV1,
			ProtocolVersionV1,
			time.Now().UTC(),
			nil,
		)
		cancelExport()
		if err != nil {
			symbol := SymbolOf(err)
			if strings.TrimSpace(symbol) == "" {
				symbol = ErrInvalidParamsSymbol
			}
			return nil, symbol, err.Error()
		}
		return rpcCardGetResult{ContactCardJSON: strings.TrimSpace(string(rawCard))}, "", ""
	case "agent.data.push":
		var params rpcDataPushParams
		if err := decodeRPCParams(req.Params, &params); err != nil {
			return nil, ErrInvalidParamsSymbol, err.Error()
		}
		params.Topic = strings.TrimSpace(params.Topic)
		params.ContentType = strings.TrimSpace(params.ContentType)
		params.PayloadBase64 = strings.TrimSpace(params.PayloadBase64)
		params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
		params.SessionID = strings.TrimSpace(params.SessionID)
		params.ReplyTo = strings.TrimSpace(params.ReplyTo)

		if params.Topic == "" {
			return nil, ErrInvalidParamsSymbol, "topic is required"
		}
		if params.ContentType == "" {
			return nil, ErrInvalidParamsSymbol, "content_type is required"
		}
		if params.PayloadBase64 == "" {
			return nil, ErrInvalidParamsSymbol, "payload_base64 is required"
		}
		if params.IdempotencyKey == "" {
			return nil, ErrInvalidParamsSymbol, "idempotency_key is required"
		}
		now := time.Now().UTC()
		if !n.allowDataPush(fromPeerID, now) {
			limit := n.opts.DataPushPerMinute
			if limit <= 0 {
				limit = DefaultDataPushRateLimit
			}
			return nil, ErrRateLimitedSymbol, fmt.Sprintf("peer exceeded rate limit: %d/min", limit)
		}

		payloadBytes, err := base64.RawURLEncoding.DecodeString(params.PayloadBase64)
		if err != nil {
			return nil, ErrInvalidParamsSymbol, "payload_base64 decode failed"
		}
		if len(payloadBytes) > n.opts.MaxPayloadBytes {
			return nil, ErrPayloadTooLargeSymbol, "payload exceeds max_payload_bytes"
		}
		sessionID, replyTo, err := validateSessionForTopic(params.Topic, params.SessionID, params.ReplyTo)
		if err != nil {
			return nil, ErrInvalidParamsSymbol, err.Error()
		}

		dedupeKey, deduped := n.reserveIncomingDedupe(fromPeerID, params.Topic, params.IdempotencyKey, now)
		if deduped {
			return rpcDataPushResult{Accepted: true, Deduped: true}, "", ""
		}

		inboxMessage := InboxMessage{
			MessageID:      uuid.NewString(),
			FromPeerID:     fromPeerID,
			Topic:          params.Topic,
			ContentType:    params.ContentType,
			PayloadBase64:  params.PayloadBase64,
			IdempotencyKey: params.IdempotencyKey,
			SessionID:      sessionID,
			ReplyTo:        replyTo,
			ReceivedAt:     now,
		}
		if err := n.enqueueIncomingPush(incomingPushEnvelope{message: inboxMessage}); err != nil {
			n.releaseIncomingDedupe(dedupeKey)
			symbol := SymbolOf(err)
			if strings.TrimSpace(symbol) == "" {
				symbol = ErrBusySymbol
			}
			return nil, symbol, err.Error()
		}

		event := DataPushEvent{
			FromPeerID:     fromPeerID,
			Topic:          params.Topic,
			ContentType:    params.ContentType,
			PayloadBase64:  params.PayloadBase64,
			PayloadBytes:   payloadBytes,
			IdempotencyKey: params.IdempotencyKey,
			SessionID:      sessionID,
			ReplyTo:        replyTo,
			ReceivedAt:     now,
			Deduped:        false,
		}
		if n.opts.OnDataPush != nil {
			n.opts.OnDataPush(event)
		}
		return rpcDataPushResult{Accepted: true, Deduped: false}, "", ""
	default:
		return nil, ErrMethodNotAllowedSymbol, "method=" + req.Method
	}
}

func (n *Node) writeRPCSuccess(stream network.Stream, id any, result any) (int, error) {
	payload, err := makeRPCSuccess(id, result)
	if err != nil {
		return 0, err
	}
	if len(payload) > n.opts.MaxRPCRequestBytes {
		return 0, WrapProtocolError(ErrPayloadTooLarge, "rpc success response too large")
	}
	written, err := stream.Write(payload)
	if err != nil {
		return written, err
	}
	return written, nil
}

func (n *Node) writeRPCError(stream network.Stream, id any, symbol string, details string) (int, error) {
	payload, err := makeRPCError(id, symbol, details)
	if err != nil {
		return 0, err
	}
	written, err := stream.Write(payload)
	if err != nil {
		return written, err
	}
	return written, nil
}

func (n *Node) ensurePeerAllowed(ctx context.Context, peerID string) (Contact, error) {
	contact, ok, err := n.svc.GetContactByPeerID(ctx, peerID)
	if err != nil {
		return Contact{}, err
	}
	if !ok {
		return Contact{}, WrapProtocolError(ErrUnauthorized, "peer is not in contacts")
	}
	switch contact.TrustState {
	case TrustStateRevoked, TrustStateConflicted:
		return Contact{}, WrapProtocolError(ErrUnauthorized, "peer trust_state=%s", contact.TrustState)
	default:
		return contact, nil
	}
}

func (n *Node) resolveDialTarget(ctx context.Context, peerID string, addresses []string) (peer.ID, []string, Contact, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return "", nil, Contact{}, WrapProtocolError(ErrInvalidParams, "peer_id is required")
	}

	contact, err := n.ensurePeerAllowed(ctx, peerID)
	if err != nil {
		return "", nil, Contact{}, err
	}

	expectedPeerID, err := peer.Decode(peerID)
	if err != nil {
		return "", nil, Contact{}, WrapProtocolError(ErrInvalidParams, "invalid peer_id: %v", err)
	}

	dialAddresses := normalizeAddresses(addresses)
	if len(dialAddresses) == 0 {
		dialAddresses = normalizeAddresses(contact.Addresses)
	}
	if len(dialAddresses) == 0 {
		return "", nil, Contact{}, WrapProtocolError(ErrInvalidParams, "no dial addresses available")
	}
	for _, addr := range dialAddresses {
		if err := validateAddressMatchesPeerID(addr, expectedPeerID); err != nil {
			return "", nil, Contact{}, err
		}
	}
	return expectedPeerID, dialAddresses, contact, nil
}

func resolveExplicitDialTarget(peerID string, addresses []string) (peer.ID, []string, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return "", nil, WrapProtocolError(ErrInvalidParams, "peer_id is required")
	}

	expectedPeerID, err := peer.Decode(peerID)
	if err != nil {
		return "", nil, WrapProtocolError(ErrInvalidParams, "invalid peer_id: %v", err)
	}

	dialAddresses := normalizeAddresses(addresses)
	if len(dialAddresses) == 0 {
		return "", nil, WrapProtocolError(ErrInvalidParams, "no dial addresses available")
	}
	for _, addr := range dialAddresses {
		if err := validateAddressMatchesPeerID(addr, expectedPeerID); err != nil {
			return "", nil, err
		}
	}
	return expectedPeerID, dialAddresses, nil
}

func (n *Node) connect(ctx context.Context, targetPeerID peer.ID, addresses []string) error {
	directAddrs, relayAddrs := splitDialAddresses(addresses)
	orderedSets := dialAddressSetsForMode(n.opts.RelayMode, directAddrs, relayAddrs)
	n.opts.Logger.Debug(
		"connect start",
		"target_peer_id", targetPeerID.String(),
		"relay_mode", n.opts.RelayMode,
		"direct_count", len(directAddrs),
		"relay_count", len(relayAddrs),
	)
	if len(orderedSets) == 0 {
		n.opts.Logger.Debug(
			"connect no dial sets",
			"target_peer_id", targetPeerID.String(),
			"relay_mode", n.opts.RelayMode,
		)
		return fmt.Errorf("connect to %s failed: no dial addresses for relay_mode=%s", targetPeerID.String(), n.opts.RelayMode)
	}

	connectErrors := make([]string, 0, len(addresses))
	directErrors := make([]string, 0, len(directAddrs))
	for i, set := range orderedSets {
		for _, raw := range set.Addresses {
			n.opts.Logger.Debug(
				"connect dial attempt",
				"target_peer_id", targetPeerID.String(),
				"path", set.Path,
				"address", raw,
			)
			addressCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.DialAddrTimeout)
			err := n.connectOneAddress(addressCtx, targetPeerID, raw)
			cancel()
			if err == nil {
				n.opts.Logger.Debug(
					"connect dial success",
					"target_peer_id", targetPeerID.String(),
					"path", set.Path,
					"address", raw,
				)
				event := RelayEvent{
					Event:        "relay.path.selected",
					Path:         set.Path,
					TargetPeerID: targetPeerID.String(),
					Timestamp:    time.Now().UTC(),
				}
				if set.Path == "relay" {
					event.RelayPeerID = relayPeerIDFromCircuitAddress(raw)
				}
				n.emitRelayEvent(event)
				return nil
			}
			n.opts.Logger.Debug(
				"connect dial failed",
				"target_peer_id", targetPeerID.String(),
				"path", set.Path,
				"address", raw,
				"err", err,
			)
			connectErrors = append(connectErrors, fmt.Sprintf("%s(%s): %v", set.Path, raw, err))
			if set.Path == "direct" {
				directErrors = append(directErrors, fmt.Sprintf("%s: %v", raw, err))
			}
		}
		if i == 0 && set.Path == "direct" && len(set.Addresses) > 0 && hasRelayAddressSet(orderedSets[1:]) {
			n.emitRelayEvent(RelayEvent{
				Event:        "relay.fallback",
				Path:         "relay",
				TargetPeerID: targetPeerID.String(),
				Reason:       strings.Join(directErrors, "; "),
				Timestamp:    time.Now().UTC(),
			})
		}
	}
	if len(connectErrors) == 0 {
		return fmt.Errorf("connect to %s failed: no dial addresses", targetPeerID.String())
	}
	n.opts.Logger.Debug(
		"connect failed",
		"target_peer_id", targetPeerID.String(),
		"errors", strings.Join(connectErrors, "; "),
	)
	return fmt.Errorf("connect to %s failed: %s", targetPeerID.String(), strings.Join(connectErrors, "; "))
}

func (n *Node) connectOneAddress(ctx context.Context, targetPeerID peer.ID, address string) error {
	info, err := dialAddrInfoForTarget(address, targetPeerID)
	if err != nil {
		return err
	}
	if err := n.host.Connect(ctx, info); err != nil {
		return err
	}
	n.opts.Logger.Debug(
		"connect address established",
		"target_peer_id", targetPeerID.String(),
		"address", strings.TrimSpace(address),
		"connectedness", n.host.Network().Connectedness(targetPeerID).String(),
		"conn_count", len(n.host.Network().ConnsToPeer(targetPeerID)),
	)
	return nil
}

func dialAddrInfoForTarget(address string, targetPeerID peer.ID) (peer.AddrInfo, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return peer.AddrInfo{}, fmt.Errorf("empty dial address")
	}
	info, err := peer.AddrInfoFromString(address)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("invalid dial multiaddr %q: %w", address, err)
	}
	if info == nil || info.ID == "" {
		return peer.AddrInfo{}, fmt.Errorf("invalid dial multiaddr %q: missing peer id", address)
	}
	if info.ID != targetPeerID {
		return peer.AddrInfo{}, fmt.Errorf("dial multiaddr %q targets %s but expected %s", address, info.ID.String(), targetPeerID.String())
	}
	if len(info.Addrs) == 0 {
		return peer.AddrInfo{}, fmt.Errorf("invalid dial multiaddr %q: missing transport address", address)
	}
	return *info, nil
}

func connDebugDetails(conns []network.Conn) []string {
	if len(conns) == 0 {
		return nil
	}
	out := make([]string, 0, len(conns))
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		state := conn.ConnState()
		out = append(out, fmt.Sprintf(
			"id=%s dir=%s limited=%v local=%s remote=%s mux=%s sec=%s transport=%s streams=%d closed=%v",
			conn.ID(),
			conn.Stat().Direction.String(),
			conn.Stat().Limited,
			conn.LocalMultiaddr().String(),
			conn.RemoteMultiaddr().String(),
			state.StreamMultiplexer,
			state.Security,
			state.Transport,
			len(conn.GetStreams()),
			conn.IsClosed(),
		))
	}
	return out
}

func splitDialAddresses(addresses []string) ([]string, []string) {
	direct := make([]string, 0, len(addresses))
	relay := make([]string, 0, len(addresses))
	for _, raw := range addresses {
		if strings.Contains(strings.ToLower(strings.TrimSpace(raw)), "/p2p-circuit") {
			relay = append(relay, raw)
		} else {
			direct = append(direct, raw)
		}
	}
	return direct, relay
}

type dialAddressSet struct {
	Path      string
	Addresses []string
}

func dialAddressSetsForMode(mode string, direct []string, relay []string) []dialAddressSet {
	switch strings.TrimSpace(mode) {
	case RelayModeOff:
		if len(direct) == 0 {
			return nil
		}
		return []dialAddressSet{{Path: "direct", Addresses: direct}}
	case RelayModeRequired:
		if len(relay) == 0 {
			return nil
		}
		return []dialAddressSet{{Path: "relay", Addresses: relay}}
	default:
		sets := make([]dialAddressSet, 0, 2)
		if len(direct) > 0 {
			sets = append(sets, dialAddressSet{Path: "direct", Addresses: direct})
		}
		if len(relay) > 0 {
			sets = append(sets, dialAddressSet{Path: "relay", Addresses: relay})
		}
		return sets
	}
}

func hasRelayAddressSet(sets []dialAddressSet) bool {
	for _, set := range sets {
		if set.Path == "relay" && len(set.Addresses) > 0 {
			return true
		}
	}
	return false
}

func relayPeerIDFromCircuitAddress(address string) string {
	maddr, err := ma.NewMultiaddr(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	relayPeerID := ""
	sawCircuit := false
	for _, component := range ma.Split(maddr) {
		switch component.Protocol().Code {
		case ma.P_CIRCUIT:
			sawCircuit = true
		case ma.P_P2P:
			if !sawCircuit {
				relayPeerID = strings.TrimSpace(component.Value())
			}
		}
		if sawCircuit {
			break
		}
	}
	if !sawCircuit {
		return ""
	}
	return relayPeerID
}

func (n *Node) reserveConfiguredRelays(ctx context.Context) error {
	infos, err := parseRelayAddrInfos(n.opts.RelayAddrs)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		n.setRelayAdvertiseAddrs(nil)
		return fmt.Errorf("no valid relay addresses configured")
	}
	if err := rejectRelayInfosForLocalPeerID(infos, n.host.ID()); err != nil {
		n.setRelayAdvertiseAddrs(nil)
		return err
	}
	n.opts.Logger.Debug(
		"relay reservation start",
		"relay_count", len(infos),
		"local_peer_id", n.host.ID().String(),
	)

	relayAddrs := make([]string, 0, len(infos))
	successCount := 0
	for _, info := range infos {
		n.opts.Logger.Debug(
			"relay reservation attempt",
			"relay_peer_id", info.ID.String(),
			"relay_addr_count", len(info.Addrs),
		)
		reserveCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.HelloTimeout)
		reservation, reserveErr := relayclient.Reserve(reserveCtx, n.host, info)
		cancel()

		if reserveErr != nil {
			n.opts.Logger.Debug(
				"relay reservation failed",
				"relay_peer_id", info.ID.String(),
				"err", reserveErr,
			)
			n.emitRelayEvent(RelayEvent{
				Event:       "relay.reservation.failed",
				Path:        "relay",
				RelayPeerID: info.ID.String(),
				Reason:      reserveErr.Error(),
				Timestamp:   time.Now().UTC(),
			})
			continue
		}

		successCount++
		addrs := make([]string, 0, len(reservation.Addrs))
		for _, addr := range reservation.Addrs {
			addrs = append(addrs, strings.TrimSpace(addr.String()))
		}
		addrs = normalizeRelayReservationAddrs(addrs, info.ID, n.opts.Logger)
		if len(addrs) == 0 {
			addrs = buildRelayCircuitBaseAddrs(info)
		}
		relayAddrs = append(relayAddrs, addrs...)
		n.opts.Logger.Debug(
			"relay reservation ok",
			"relay_peer_id", info.ID.String(),
			"relay_advertise_count", len(addrs),
		)

		n.emitRelayEvent(RelayEvent{
			Event:       "relay.reservation.ok",
			Path:        "relay",
			RelayPeerID: info.ID.String(),
			Reason:      fmt.Sprintf("expires_at=%s", reservation.Expiration.UTC().Format(time.RFC3339)),
			Timestamp:   time.Now().UTC(),
		})
	}

	n.setRelayAdvertiseAddrs(relayAddrs)
	if successCount == 0 {
		return fmt.Errorf("no relay reservation succeeded")
	}
	n.opts.Logger.Debug(
		"relay reservation completed",
		"success_count", successCount,
		"relay_advertise_total", len(relayAddrs),
	)
	return nil
}

func (n *Node) setRelayAdvertiseAddrs(addrs []string) {
	n.relayMu.Lock()
	n.relayAdvertiseAddrs = normalizeAddresses(addrs)
	n.relayMu.Unlock()
}

func (n *Node) relayAdvertiseAddrsSnapshot() []string {
	n.relayMu.RLock()
	defer n.relayMu.RUnlock()
	if len(n.relayAdvertiseAddrs) == 0 {
		return nil
	}
	out := make([]string, len(n.relayAdvertiseAddrs))
	copy(out, n.relayAdvertiseAddrs)
	return out
}

func (n *Node) emitRelayEvent(event RelayEvent) {
	if n == nil || n.opts.OnRelayEvent == nil {
		return
	}
	event.Event = strings.TrimSpace(event.Event)
	event.Path = strings.TrimSpace(event.Path)
	event.RelayPeerID = strings.TrimSpace(event.RelayPeerID)
	event.TargetPeerID = strings.TrimSpace(event.TargetPeerID)
	event.Reason = strings.TrimSpace(event.Reason)
	if event.Event == "" {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	n.opts.OnRelayEvent(event)
}

func parseRelayAddrInfos(rawAddresses []string) ([]peer.AddrInfo, error) {
	type relayInfoBuilder struct {
		id    peer.ID
		addrs map[string]ma.Multiaddr
	}

	builders := map[string]*relayInfoBuilder{}
	for _, raw := range normalizeAddresses(rawAddresses) {
		lower := strings.ToLower(strings.TrimSpace(raw))
		if strings.Contains(lower, "/p2p-circuit") {
			return nil, fmt.Errorf("relay address %q must not include /p2p-circuit", raw)
		}
		info, err := peer.AddrInfoFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid relay address %q: %w", raw, err)
		}
		if info == nil || info.ID == "" {
			return nil, fmt.Errorf("invalid relay address %q: missing relay peer id", raw)
		}
		if len(info.Addrs) == 0 {
			return nil, fmt.Errorf("invalid relay address %q: missing transport address", raw)
		}

		key := info.ID.String()
		builder, ok := builders[key]
		if !ok {
			builder = &relayInfoBuilder{
				id:    info.ID,
				addrs: map[string]ma.Multiaddr{},
			}
			builders[key] = builder
		}
		for _, addr := range info.Addrs {
			builder.addrs[addr.String()] = addr
		}
	}

	if len(builders) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(builders))
	for key := range builders {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]peer.AddrInfo, 0, len(keys))
	for _, key := range keys {
		builder := builders[key]
		addrs := make([]ma.Multiaddr, 0, len(builder.addrs))
		for _, addr := range builder.addrs {
			addrs = append(addrs, addr)
		}
		sort.Slice(addrs, func(i, j int) bool {
			return addrs[i].String() < addrs[j].String()
		})
		out = append(out, peer.AddrInfo{
			ID:    builder.id,
			Addrs: addrs,
		})
	}
	return out, nil
}

func rejectRelayInfosForLocalPeerID(infos []peer.AddrInfo, localPeerID peer.ID) error {
	if localPeerID == "" || len(infos) == 0 {
		return nil
	}
	for _, info := range infos {
		if info.ID != "" && info.ID == localPeerID {
			return fmt.Errorf("relay peer_id %s matches local peer_id; use a dedicated relay identity (set --dir or AQUA_DIR)", localPeerID.String())
		}
	}
	return nil
}

func buildRelayCircuitBaseAddrs(info peer.AddrInfo) []string {
	if info.ID == "" || len(info.Addrs) == 0 {
		return nil
	}
	relayComponent, err := ma.NewMultiaddr("/p2p/" + info.ID.String())
	if err != nil {
		return nil
	}
	circuitComponent, err := ma.NewMultiaddr("/p2p-circuit")
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(info.Addrs))
	for _, relayAddr := range info.Addrs {
		out = append(out, relayAddr.Encapsulate(relayComponent).Encapsulate(circuitComponent).String())
	}
	return normalizeAddresses(out)
}

func normalizeRelayReservationAddrs(rawAddrs []string, relayPeerID peer.ID, logger *slog.Logger) []string {
	if relayPeerID == "" {
		return nil
	}
	out := make([]string, 0, len(rawAddrs))
	for _, raw := range normalizeAddresses(rawAddrs) {
		canonical, err := canonicalRelayCircuitBaseAddr(raw, relayPeerID)
		if err != nil {
			if logger != nil {
				logger.Debug("ignore invalid relay reservation addr", "raw", raw, "relay_peer_id", relayPeerID.String(), "err", err)
			}
			continue
		}
		out = append(out, canonical)
	}
	return normalizeAddresses(out)
}

func canonicalRelayCircuitBaseAddr(raw string, relayPeerID peer.ID) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty relay reservation address")
	}
	maddr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return "", fmt.Errorf("invalid relay reservation address %q: %w", raw, err)
	}
	parts := ma.Split(maddr)
	transportParts := make([]ma.Multiaddr, 0, len(parts))
	for i := range parts {
		part := &parts[i]
		switch part.Protocol().Code {
		case ma.P_P2P, ma.P_CIRCUIT:
			goto build
		default:
			transportParts = append(transportParts, part.Multiaddr())
		}
	}

build:
	if len(transportParts) == 0 {
		return "", fmt.Errorf("relay reservation address %q missing transport part", raw)
	}
	base := transportParts[0]
	for i := 1; i < len(transportParts); i++ {
		base = base.Encapsulate(transportParts[i])
	}
	relayComponent, err := ma.NewMultiaddr("/p2p/" + relayPeerID.String())
	if err != nil {
		return "", err
	}
	circuitComponent, err := ma.NewMultiaddr("/p2p-circuit")
	if err != nil {
		return "", err
	}
	return base.Encapsulate(relayComponent).Encapsulate(circuitComponent).String(), nil
}

func (n *Node) hasFreshSession(peerID string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	session, ok := n.sessions[strings.TrimSpace(peerID)]
	if !ok {
		return false
	}
	if session.NegotiatedProtocol <= 0 || session.UpdatedAt.IsZero() {
		return false
	}
	return time.Since(session.UpdatedAt) <= sessionFreshWindow
}

func (n *Node) allowDataPush(peerID string, now time.Time) bool {
	limit := n.opts.DataPushPerMinute
	if limit <= 0 {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	windowMinute := now.UTC().Truncate(time.Minute)
	peerID = strings.TrimSpace(peerID)

	n.rateMu.Lock()
	defer n.rateMu.Unlock()

	window := n.pushRateWindows[peerID]
	if window.WindowMinute.IsZero() || !window.WindowMinute.Equal(windowMinute) {
		window = pushRateWindow{WindowMinute: windowMinute, Count: 0}
	}
	if window.Count >= limit {
		n.pushRateWindows[peerID] = window
		return false
	}
	window.Count++
	n.pushRateWindows[peerID] = window
	return true
}

func (n *Node) recordSession(result HelloResult) error {
	peerID := strings.TrimSpace(result.RemotePeerID)
	if peerID == "" {
		return fmt.Errorf("empty remote peer id")
	}
	now := time.Now().UTC()
	result.UpdatedAt = now
	n.mu.Lock()
	n.sessions[peerID] = result
	n.mu.Unlock()
	return nil
}

func parseHelloMessage(raw []byte) (helloMessage, error) {
	var msg helloMessage
	if err := decodeRPCJSON(raw, &msg); err != nil {
		return helloMessage{}, err
	}
	msg.Type = strings.TrimSpace(msg.Type)
	if msg.Type != "" && msg.Type != "hello" {
		return helloMessage{}, WrapProtocolError(ErrInvalidParams, "hello.type must be \"hello\"")
	}
	if msg.ProtocolMin <= 0 || msg.ProtocolMax <= 0 {
		return helloMessage{}, WrapProtocolError(ErrInvalidParams, "hello protocol range must be positive")
	}
	if msg.ProtocolMin > msg.ProtocolMax {
		return helloMessage{}, WrapProtocolError(ErrInvalidParams, "hello protocol_min cannot exceed protocol_max")
	}
	if len(msg.Capabilities) == 0 {
		msg.Capabilities = []string{}
	}
	return msg, nil
}

func (n *Node) localHelloMessage() helloMessage {
	return helloMessage{
		Type:         "hello",
		ProtocolMin:  ProtocolVersionV1,
		ProtocolMax:  ProtocolVersionV1,
		Capabilities: []string{CapabilityDataPushV1},
	}
}

func negotiateProtocol(localMin int, localMax int, remoteMin int, remoteMax int) (int, error) {
	negotiated := localMax
	if remoteMax < negotiated {
		negotiated = remoteMax
	}
	requiredMin := localMin
	if remoteMin > requiredMin {
		requiredMin = remoteMin
	}
	if negotiated < requiredMin {
		return 0, WrapProtocolError(ErrUnsupportedProtocol, "no protocol overlap")
	}
	return negotiated, nil
}

func verifyRemotePeerOnStream(stream network.Stream, expected peer.ID) error {
	actual := stream.Conn().RemotePeer()
	if actual != expected {
		return WrapProtocolError(ErrPeerIDMismatch, "remote peer mismatch: expected=%s actual=%s", expected.String(), actual.String())
	}
	remotePub := stream.Conn().RemotePublicKey()
	if remotePub == nil {
		return WrapProtocolError(ErrPeerIDMismatch, "remote public key is missing")
	}
	derived, err := peer.IDFromPublicKey(remotePub)
	if err != nil {
		return WrapProtocolError(ErrPeerIDMismatch, "derive peer id from remote public key failed: %v", err)
	}
	if derived != expected {
		return WrapProtocolError(ErrPeerIDMismatch, "remote public key peer id mismatch")
	}
	return nil
}

func readAllLimited(reader io.Reader, maxBytes int) ([]byte, bool, error) {
	if maxBytes <= 0 {
		maxBytes = MaxRPCRequestBytesV1
	}
	limited := io.LimitReader(reader, int64(maxBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxBytes {
		return data, true, nil
	}
	return data, false, nil
}

func validateSessionForTopic(topic string, sessionID string, replyTo string) (string, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	replyTo = strings.TrimSpace(replyTo)
	if IsDialogueTopic(topic) && sessionID == "" {
		return "", "", fmt.Errorf("session_id is required for dialogue topics")
	}
	if sessionID != "" {
		if err := validateSessionID(sessionID); err != nil {
			return "", "", err
		}
	}
	return sessionID, replyTo, nil
}

func validateSessionID(sessionID string) error {
	id, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	if id.Version() != uuid.Version(7) {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	return nil
}

func protocolErrorFromSymbol(symbol string, details string) error {
	symbol = strings.TrimSpace(symbol)
	details = strings.TrimSpace(details)
	base := protocolErrorBySymbol(symbol)
	if details == "" {
		return base
	}
	return WrapProtocolError(base, "%s", details)
}

func protocolErrorBySymbol(symbol string) *ProtocolError {
	switch symbol {
	case ErrUnauthorizedSymbol:
		return ErrUnauthorized
	case ErrPeerIDMismatchSymbol:
		return ErrPeerIDMismatch
	case ErrContactConflictedSymbol:
		return ErrContactConflicted
	case ErrMethodNotAllowedSymbol:
		return ErrMethodNotAllowed
	case ErrPayloadTooLargeSymbol:
		return ErrPayloadTooLarge
	case ErrRateLimitedSymbol:
		return ErrRateLimited
	case ErrBusySymbol:
		return ErrBusy
	case ErrUnsupportedProtocolSymbol:
		return ErrUnsupportedProtocol
	case ErrInvalidJSONProfileSymbol:
		return ErrInvalidJSONProfile
	case ErrInvalidContactCardSymbol:
		return ErrInvalidContactCard
	case ErrInvalidParamsSymbol:
		return ErrInvalidParams
	default:
		return &ProtocolError{Symbol: symbol, Message: symbol}
	}
}

func normalizeNodeOptions(opts NodeOptions) (NodeOptions, error) {
	relayMode, err := normalizeRelayMode(opts.RelayMode)
	if err != nil {
		return NodeOptions{}, err
	}
	opts.RelayMode = relayMode
	opts.RelayAddrs = normalizeAddresses(opts.RelayAddrs)
	if len(opts.RelayAddrs) > 0 {
		if _, err := parseRelayAddrInfos(opts.RelayAddrs); err != nil {
			return NodeOptions{}, err
		}
	}

	if opts.DialAddrTimeout <= 0 {
		opts.DialAddrTimeout = DefaultDialAddrTimeout
	}
	if opts.HelloTimeout <= 0 {
		opts.HelloTimeout = DefaultHelloTimeout
	}
	if opts.RPCTimeout <= 0 {
		opts.RPCTimeout = DefaultRPCTimeout
	}
	if opts.MaxRPCRequestBytes <= 0 {
		opts.MaxRPCRequestBytes = MaxRPCRequestBytesV1
	}
	if opts.MaxPayloadBytes <= 0 {
		opts.MaxPayloadBytes = MaxPayloadBytesV1
	}
	if opts.DataPushPerMinute <= 0 {
		opts.DataPushPerMinute = DefaultDataPushRateLimit
	}
	if opts.DedupeTTL <= 0 {
		opts.DedupeTTL = DefaultDedupeTTL
	}
	if opts.DedupeMaxEntries <= 0 {
		opts.DedupeMaxEntries = DefaultDedupeMaxEntries
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if !opts.DialOnly {
		opts.ListenAddrs = normalizeAddresses(opts.ListenAddrs)
		if len(opts.ListenAddrs) == 0 {
			opts.ListenAddrs = defaultPreferredListenAddrs()
		}
	}
	return opts, nil
}

func normalizeRelayMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return RelayModeAuto, nil
	}
	switch mode {
	case RelayModeAuto, RelayModeOff, RelayModeRequired:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid relay mode %q (supported: %s, %s, %s)", raw, RelayModeAuto, RelayModeOff, RelayModeRequired)
	}
}

func defaultPreferredListenAddrs() []string {
	return []string{
		"/ip4/0.0.0.0/udp/6372/quic-v1",
		"/ip4/0.0.0.0/tcp/6372",
	}
}

func defaultFallbackListenAddrs() []string {
	return []string{
		"/ip4/0.0.0.0/udp/0/quic-v1",
		"/ip4/0.0.0.0/tcp/0",
		"/ip4/0.0.0.0/tcp/0/ws",
	}
}

func newLibp2pHost(priv ic.PrivKey, dialOnly bool, listenAddrs []string) (host.Host, error) {
	hostOpts := []libp2p.Option{libp2p.Identity(priv)}
	hostOpts = append(hostOpts, libp2pNetworkOptions(dialOnly, listenAddrs)...)
	return libp2p.New(hostOpts...)
}

func libp2pNetworkOptions(dialOnly bool, listenAddrs []string) []libp2p.Option {
	if dialOnly {
		return []libp2p.Option{
			libp2p.NoListenAddrs,
			// NoListenAddrs disables relay transport unless explicitly enabled.
			// Dial-only commands still need relay transport for /p2p-circuit dials.
			libp2p.EnableRelay(),
		}
	}
	return []libp2p.Option{libp2p.ListenAddrStrings(listenAddrs...)}
}

func withTimeoutIfNeeded(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func sortStrings(values []string) {
	if len(values) <= 1 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
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

func discoverInterfaceIPs() ([]net.IP, error) {
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

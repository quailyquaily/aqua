package maep

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
)

const sessionFreshWindow = 10 * time.Minute

type NodeOptions struct {
	DialOnly           bool
	ListenAddrs        []string
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
}

type pushRateWindow struct {
	WindowMinute time.Time
	Count        int
}

type helloMessage struct {
	Type         string   `json:"type"`
	ProtocolMin  int      `json:"protocol_min"`
	ProtocolMax  int      `json:"protocol_max"`
	Capabilities []string `json:"capabilities"`
}

func NewNode(ctx context.Context, svc *Service, opts NodeOptions) (*Node, error) {
	if svc == nil || svc.store == nil {
		return nil, fmt.Errorf("nil maep service")
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

	options := normalizeNodeOptions(opts)

	hostOpts := []libp2p.Option{libp2p.Identity(priv)}
	if options.DialOnly {
		hostOpts = append(hostOpts, libp2p.NoListenAddrs)
	} else {
		hostOpts = append(hostOpts, libp2p.ListenAddrStrings(options.ListenAddrs...))
	}

	h, err := libp2p.New(hostOpts...)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	if h.ID().String() != identity.PeerID {
		_ = h.Close()
		return nil, fmt.Errorf("libp2p host identity mismatch: host=%s identity=%s", h.ID().String(), identity.PeerID)
	}

	n := &Node{
		host:            h,
		svc:             svc,
		store:           svc.store,
		local:           identity,
		opts:            options,
		sessions:        map[string]HelloResult{},
		pushRateWindows: map[string]pushRateWindow{},
	}

	h.SetStreamHandler(protocol.ID(ProtocolHelloIDV1), n.handleHelloStream)
	h.SetStreamHandler(protocol.ID(ProtocolRPCIDV1), n.handleRPCStream)

	return n, nil
}

func (n *Node) Close() error {
	if n == nil || n.host == nil {
		return nil
	}
	return n.host.Close()
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
	out := make([]string, 0, len(baseAddrs))
	for _, addr := range baseAddrs {
		p2pComponent, err := ma.NewMultiaddr("/p2p/" + n.host.ID().String())
		if err != nil {
			continue
		}
		out = append(out, addr.Encapsulate(p2pComponent).String())
	}
	sortStrings(out)
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
	if err := n.store.AppendOutboxMessage(context.Background(), outboxMessage); err != nil {
		n.opts.Logger.Warn("append outbox message failed", "peer_id", peerID, "err", err)
	}

	return result, nil
}

func (n *Node) DialHello(ctx context.Context, peerID string, addresses []string) (HelloResult, error) {
	expectedPeerID, dialAddresses, _, err := n.resolveDialTarget(ctx, peerID, addresses)
	if err != nil {
		return HelloResult{}, err
	}

	timeoutCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.HelloTimeout)
	defer cancel()

	if err := n.connect(timeoutCtx, expectedPeerID, dialAddresses); err != nil {
		return HelloResult{}, err
	}

	stream, err := n.host.NewStream(timeoutCtx, expectedPeerID, protocol.ID(ProtocolHelloIDV1))
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

	if !n.hasFreshSession(expectedPeerID.String()) {
		if _, err := n.DialHello(ctx, expectedPeerID.String(), dialAddresses); err != nil {
			return nil, err
		}
	}

	timeoutCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.RPCTimeout)
	defer cancel()

	if err := n.connect(timeoutCtx, expectedPeerID, dialAddresses); err != nil {
		return nil, err
	}

	stream, err := n.host.NewStream(timeoutCtx, expectedPeerID, protocol.ID(ProtocolRPCIDV1))
	if err != nil {
		return nil, fmt.Errorf("open rpc stream: %w", err)
	}
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
	if strings.TrimSpace(symbol) != "" {
		// Handles session drift (for example remote node restart) by renegotiating once.
		if shouldRetryAfterUnsupported(symbol, retriedUnsupported) {
			if _, err := n.DialHello(ctx, expectedPeerID.String(), dialAddresses); err != nil {
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

func (n *Node) handleHelloStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().UTC().Add(n.opts.HelloTimeout))

	remotePeer := stream.Conn().RemotePeer().String()
	if _, err := n.ensurePeerAllowed(context.Background(), remotePeer); err != nil {
		n.opts.Logger.Warn("reject hello from unauthorized peer", "peer_id", remotePeer, "err", err)
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

	if !n.hasFreshSession(remotePeerID) {
		if req.HasID {
			_, _ = n.writeRPCError(stream, req.ID, ErrUnsupportedProtocolSymbol, "hello negotiation required before rpc")
		}
		return
	}

	if _, err := n.ensurePeerAllowed(context.Background(), remotePeerID); err != nil {
		if req.HasID {
			_, _ = n.writeRPCError(stream, req.ID, ErrUnauthorizedSymbol, err.Error())
		}
		_ = stream.Conn().Close()
		return
	}

	if !isAllowedMethod(req.Method) {
		if req.HasID {
			_, _ = n.writeRPCError(stream, req.ID, ErrMethodNotAllowedSymbol, "method="+req.Method)
		}
		return
	}

	result, symbol, details := n.handleRPCMethod(remotePeerID, req)
	if !req.HasID {
		if symbol != "" {
			n.opts.Logger.Warn("rpc notification rejected", "peer_id", remotePeerID, "method", req.Method, "symbol", symbol, "details", details)
		}
		return
	}

	if strings.TrimSpace(symbol) != "" {
		_, _ = n.writeRPCError(stream, req.ID, symbol, details)
		return
	}
	_, _ = n.writeRPCSuccess(stream, req.ID, result)
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

		deduped := false
		if _, exists, err := n.store.GetDedupeRecord(context.Background(), fromPeerID, params.Topic, params.IdempotencyKey); err != nil {
			n.opts.Logger.Warn("dedupe lookup failed", "peer_id", fromPeerID, "err", err)
		} else if exists {
			deduped = true
		}
		if !deduped {
			record := DedupeRecord{
				FromPeerID:     fromPeerID,
				Topic:          params.Topic,
				IdempotencyKey: params.IdempotencyKey,
				CreatedAt:      now,
				ExpiresAt:      now.Add(n.opts.DedupeTTL),
			}
			if err := n.store.PutDedupeRecord(context.Background(), record); err != nil {
				n.opts.Logger.Warn("dedupe save failed", "peer_id", fromPeerID, "err", err)
			}
			if _, err := n.store.PruneDedupeRecords(context.Background(), now, n.opts.DedupeMaxEntries); err != nil {
				n.opts.Logger.Warn("dedupe prune failed", "peer_id", fromPeerID, "err", err)
			}
		}

		if !deduped {
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
			if err := n.store.AppendInboxMessage(context.Background(), inboxMessage); err != nil {
				n.opts.Logger.Warn("append inbox message failed", "peer_id", fromPeerID, "err", err)
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
		}
		return rpcDataPushResult{Accepted: true, Deduped: deduped}, "", ""
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

func (n *Node) connect(ctx context.Context, targetPeerID peer.ID, addresses []string) error {
	directAddrs, relayAddrs := splitDialAddresses(addresses)
	orderedSets := [][]string{directAddrs, relayAddrs}
	setLabels := []string{"direct", "relay"}

	connectErrors := make([]string, 0, len(addresses))
	for i, addressSet := range orderedSets {
		for _, raw := range addressSet {
			addressCtx, cancel := withTimeoutIfNeeded(ctx, n.opts.DialAddrTimeout)
			err := n.connectOneAddress(addressCtx, targetPeerID, raw)
			cancel()
			if err == nil {
				return nil
			}
			connectErrors = append(connectErrors, fmt.Sprintf("%s(%s): %v", setLabels[i], raw, err))
		}
	}
	if len(connectErrors) == 0 {
		return fmt.Errorf("connect to %s failed: no dial addresses", targetPeerID.String())
	}
	return fmt.Errorf("connect to %s failed: %s", targetPeerID.String(), strings.Join(connectErrors, "; "))
}

func (n *Node) connectOneAddress(ctx context.Context, targetPeerID peer.ID, address string) error {
	addr, err := ma.NewMultiaddr(address)
	if err != nil {
		return fmt.Errorf("invalid dial multiaddr %q: %w", address, err)
	}
	info := peer.AddrInfo{
		ID:    targetPeerID,
		Addrs: []ma.Multiaddr{addr},
	}
	if err := n.host.Connect(ctx, info); err != nil {
		return err
	}
	return nil
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

func normalizeNodeOptions(opts NodeOptions) NodeOptions {
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
			opts.ListenAddrs = []string{
				"/ip4/0.0.0.0/udp/0/quic-v1",
				"/ip4/0.0.0.0/tcp/0",
			}
		}
	}
	return opts
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

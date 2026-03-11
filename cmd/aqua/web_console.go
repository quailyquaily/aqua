package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/quailyquaily/aqua/aqua"
)

const (
	webConsoleListenDefault     = "127.0.0.1:8089"
	webConsoleRelayHistoryLimit = 32
	webConsoleHubBuffer         = 32
	webConsoleReadBodyLimit     = 1 << 20
)

type webConsoleConfig struct {
	ListenAddr  string
	StaticDir   string
	ListenAddrs []string
	RelayAddrs  []string
	RelayMode   string
	RelayProbe  bool
}

type webConsoleServer struct {
	cfg    webConsoleConfig
	svc    *aqua.Service
	node   *aqua.Node
	logger *slog.Logger
	hub    *webConsoleEventHub

	startedAt time.Time
	upgrader  websocket.Upgrader

	mu          sync.RWMutex
	relayEvents []aqua.RelayEvent
}

type webConsoleEventHub struct {
	mu          sync.RWMutex
	nextID      int
	subscribers map[int]chan []byte
}

type webConsoleNodeRuntimeView struct {
	Status            string            `json:"status"`
	StartedAt         time.Time         `json:"started_at"`
	Version           string            `json:"version"`
	Commit            string            `json:"commit"`
	Date              string            `json:"date"`
	NodeID            map[string]any    `json:"node"`
	ListenAddrs       []string          `json:"listen_addrs"`
	Addresses         []string          `json:"addresses"`
	RelayAddrs        []string          `json:"relay_addrs"`
	RelayMode         string            `json:"relay_mode"`
	RelayProbe        bool              `json:"relay_probe"`
	WebConsoleAddr    string            `json:"web_console_addr"`
	ConsoleWebDstDir  string            `json:"console_web_dst_dir"`
	UnreadCount       int               `json:"unread_count"`
	ContactsCount     int               `json:"contacts_count"`
	GroupsCount       int               `json:"groups_count"`
	RecentRelayEvents []aqua.RelayEvent `json:"recent_relay_events"`
}

type webConsoleErrorResponse struct {
	Error webConsoleErrorBody `json:"error"`
}

type webConsoleErrorBody struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Details    string `json:"details,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type webConsoleSetNicknameRequest struct {
	Nickname string `json:"nickname"`
}

type webConsoleFetchContactRequest struct {
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
	Verify      bool   `json:"verify"`
}

type webConsoleImportContactRequest struct {
	DisplayName     string          `json:"display_name"`
	ContactCardJSON json.RawMessage `json:"contact_card_json"`
}

type webConsoleMarkReadRequest struct {
	All        bool     `json:"all"`
	MessageIDs []string `json:"message_ids"`
	FromPeerID string   `json:"from_peer_id"`
	Topic      string   `json:"topic"`
}

type webConsoleSendDirectRequest struct {
	PeerID         string `json:"peer_id"`
	Message        string `json:"message"`
	Topic          string `json:"topic"`
	ContentType    string `json:"content_type"`
	IdempotencyKey string `json:"idempotency_key"`
	SessionID      string `json:"session_id"`
	ReplyTo        string `json:"reply_to"`
	Notification   bool   `json:"notification"`
}

type webConsoleGroupInviteRequest struct {
	PeerID    string `json:"peer_id"`
	LocalOnly bool   `json:"local_only"`
}

type webConsoleGroupInviteDecisionRequest struct {
	LocalOnly bool `json:"local_only"`
}

type webConsoleGroupMessageRequest struct {
	Message     string `json:"message"`
	ContentType string `json:"content_type"`
}

type webConsoleReadStateEvent struct {
	Event      string    `json:"event"`
	MessageIDs []string  `json:"message_ids"`
	Marked     int       `json:"marked"`
	Timestamp  time.Time `json:"timestamp"`
}

func startWebConsoleServer(
	cfg webConsoleConfig,
	svc *aqua.Service,
	node *aqua.Node,
	logger *slog.Logger,
) (string, *webConsoleServer, func(context.Context) error, error) {
	staticDir, err := validateWebConsoleStaticDir(cfg.StaticDir)
	if err != nil {
		return "", nil, nil, err
	}
	cfg.StaticDir = staticDir
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = webConsoleListenDefault
	}
	cfg.ListenAddrs = normalizeAddressList(cfg.ListenAddrs)
	cfg.RelayAddrs = normalizeAddressList(cfg.RelayAddrs)

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return "", nil, nil, fmt.Errorf("listen web console http server on %q: %w", cfg.ListenAddr, err)
	}
	cfg.ListenAddr = listener.Addr().String()

	server := &webConsoleServer{
		cfg:       cfg,
		svc:       svc,
		node:      node,
		logger:    logger,
		hub:       newWebConsoleEventHub(),
		startedAt: time.Now().UTC(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	httpServer := &http.Server{
		Handler:           newWebConsoleHTTPHandler(server),
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdown := runRelayHTTPServer(listener, httpServer, logger, "web console server")
	return listener.Addr().String(), server, shutdown, nil
}

func validateWebConsoleStaticDir(raw string) (string, error) {
	path := expandHomePath(strings.TrimSpace(raw))
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat --console-web-dst-dir %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--console-web-dst-dir %q is not a directory", path)
	}
	indexPath := filepath.Join(path, "index.html")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		return "", fmt.Errorf("missing index.html in --console-web-dst-dir %q: %w", path, err)
	}
	if indexInfo.IsDir() {
		return "", fmt.Errorf("index.html in --console-web-dst-dir %q is a directory", path)
	}
	return path, nil
}

func newWebConsoleEventHub() *webConsoleEventHub {
	return &webConsoleEventHub{
		subscribers: make(map[int]chan []byte),
	}
}

func (h *webConsoleEventHub) subscribe() (int, chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	ch := make(chan []byte, webConsoleHubBuffer)
	h.subscribers[id] = ch
	return id, ch
}

func (h *webConsoleEventHub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.subscribers[id]
	if !ok {
		return
	}
	delete(h.subscribers, id)
	close(ch)
}

func (h *webConsoleEventHub) publish(v any) {
	if h == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- raw:
		default:
		}
	}
}

func (s *webConsoleServer) recordDataPush(event aqua.DataPushEvent) {
	if s == nil {
		return
	}
	s.hub.publish(webConsoleDataPushEventView(event))
}

func (s *webConsoleServer) recordRelayEvent(event aqua.RelayEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.relayEvents = append(s.relayEvents, event)
	if len(s.relayEvents) > webConsoleRelayHistoryLimit {
		s.relayEvents = append([]aqua.RelayEvent(nil), s.relayEvents[len(s.relayEvents)-webConsoleRelayHistoryLimit:]...)
	}
	s.mu.Unlock()
	s.hub.publish(webConsoleRelayEventView(event))
}

func (s *webConsoleServer) recordReadStateChange(messageIDs []string, marked int) {
	if s == nil || marked <= 0 {
		return
	}
	s.hub.publish(webConsoleReadStateEvent{
		Event:      "inbox.read_state.changed",
		MessageIDs: append([]string(nil), messageIDs...),
		Marked:     marked,
		Timestamp:  time.Now().UTC(),
	})
}

func (s *webConsoleServer) runtimeView(ctx context.Context) (webConsoleNodeRuntimeView, error) {
	identity, ok, err := s.svc.GetIdentity(ctx)
	if err != nil {
		return webConsoleNodeRuntimeView{}, err
	}
	if !ok {
		return webConsoleNodeRuntimeView{}, fmt.Errorf("identity is not initialized; run `aqua init`")
	}

	contacts, err := s.svc.ListContacts(ctx)
	if err != nil {
		return webConsoleNodeRuntimeView{}, err
	}
	groups, err := s.svc.ListGroups(ctx)
	if err != nil {
		return webConsoleNodeRuntimeView{}, err
	}
	inbox, err := s.svc.ListInboxMessages(ctx, "", "", 0)
	if err != nil {
		return webConsoleNodeRuntimeView{}, err
	}
	unreadCount := 0
	for _, record := range inbox {
		if !record.Read {
			unreadCount++
		}
	}

	s.mu.RLock()
	relayEvents := append([]aqua.RelayEvent(nil), s.relayEvents...)
	s.mu.RUnlock()

	addresses := []string{}
	peerID := identity.PeerID
	if s.node != nil {
		addresses = s.node.AddrStrings()
		if strings.TrimSpace(s.node.PeerID()) != "" {
			peerID = s.node.PeerID()
		}
	}

	return webConsoleNodeRuntimeView{
		Status:    "ready",
		StartedAt: s.startedAt,
		Version:   buildVersion,
		Commit:    buildCommit,
		Date:      buildDate,
		NodeID: map[string]any{
			"peer_id":              peerID,
			"node_uuid":            identity.NodeUUID,
			"node_id":              identity.NodeID,
			"nickname":             identity.Nickname,
			"identity_pub_ed25519": identity.IdentityPubEd25519,
		},
		ListenAddrs:       append([]string(nil), s.cfg.ListenAddrs...),
		Addresses:         append([]string(nil), addresses...),
		RelayAddrs:        append([]string(nil), s.cfg.RelayAddrs...),
		RelayMode:         s.cfg.RelayMode,
		RelayProbe:        s.cfg.RelayProbe,
		WebConsoleAddr:    s.cfg.ListenAddr,
		ConsoleWebDstDir:  s.cfg.StaticDir,
		UnreadCount:       unreadCount,
		ContactsCount:     len(contacts),
		GroupsCount:       len(groups),
		RecentRelayEvents: relayEvents,
	}, nil
}

func newWebConsoleHTTPHandler(server *webConsoleServer) http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/console/v1/_hc", server.handleHealthCheck)
	apiMux.HandleFunc("GET /api/console/v1/node/id", server.handleNodeIDGet)
	apiMux.HandleFunc("PUT /api/console/v1/node/id", server.handleNodeIDPut)
	apiMux.HandleFunc("GET /api/console/v1/node/runtime", server.handleNodeRuntimeGet)
	apiMux.HandleFunc("GET /api/console/v1/node/card", server.handleNodeCardGet)
	apiMux.HandleFunc("GET /api/console/v1/contacts", server.handleContactsList)
	apiMux.HandleFunc("POST /api/console/v1/contacts/fetch", server.handleContactsFetch)
	apiMux.HandleFunc("POST /api/console/v1/contacts/import", server.handleContactsImport)
	apiMux.HandleFunc("GET /api/console/v1/contacts/{peerID}", server.handleContactShow)
	apiMux.HandleFunc("DELETE /api/console/v1/contacts/{peerID}", server.handleContactDelete)
	apiMux.HandleFunc("POST /api/console/v1/contacts/{peerID}/verify", server.handleContactVerify)
	apiMux.HandleFunc("GET /api/console/v1/inbox", server.handleInboxList)
	apiMux.HandleFunc("POST /api/console/v1/inbox/mark-read", server.handleInboxMarkRead)
	apiMux.HandleFunc("GET /api/console/v1/outbox", server.handleOutboxList)
	apiMux.HandleFunc("POST /api/console/v1/send/direct", server.handleSendDirect)
	apiMux.HandleFunc("GET /api/console/v1/groups", server.handleGroupList)
	apiMux.HandleFunc("POST /api/console/v1/groups", server.handleGroupCreate)
	apiMux.HandleFunc("GET /api/console/v1/groups/invites", server.handleGroupInvitesList)
	apiMux.HandleFunc("GET /api/console/v1/groups/{groupID}", server.handleGroupShow)
	apiMux.HandleFunc("POST /api/console/v1/groups/{groupID}/invites", server.handleGroupInviteCreate)
	apiMux.HandleFunc("POST /api/console/v1/groups/{groupID}/invites/{inviteID}/accept", server.handleGroupInviteAccept)
	apiMux.HandleFunc("POST /api/console/v1/groups/{groupID}/invites/{inviteID}/reject", server.handleGroupInviteReject)
	apiMux.HandleFunc("POST /api/console/v1/groups/{groupID}/messages", server.handleGroupMessageSend)
	apiMux.HandleFunc("GET /api/console/v1/ws/events", server.handleEventsWS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/console/v1/") {
			writeWebConsoleCORSHeaders(w, r)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			apiMux.ServeHTTP(w, r)
			return
		}
		if strings.TrimSpace(server.cfg.StaticDir) == "" {
			http.NotFound(w, r)
			return
		}
		staticHandler := newWebConsoleStaticHandler(server.cfg.StaticDir)
		staticHandler.ServeHTTP(w, r)
	})
}

func writeWebConsoleCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = "*"
	}
	requestHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
	if requestHeaders == "" {
		requestHeaders = "Authorization, Content-Type"
	}
	headers := w.Header()
	headers.Set("Access-Control-Allow-Origin", origin)
	headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	headers.Set("Access-Control-Allow-Headers", requestHeaders)
	appendVaryHeader(headers, "Origin")
	appendVaryHeader(headers, "Access-Control-Request-Method")
	appendVaryHeader(headers, "Access-Control-Request-Headers")
}

func appendVaryHeader(headers http.Header, value string) {
	if !slices.Contains(headers.Values("Vary"), value) {
		headers.Add("Vary", value)
	}
}

func newWebConsoleStaticHandler(root string) http.Handler {
	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		clean := pathForStaticRequest(r.URL.Path)
		full := filepath.Join(root, clean)
		if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

func pathForStaticRequest(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" || path == "/" {
		return "index.html"
	}
	path = strings.TrimPrefix(path, "/")
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." {
		return "index.html"
	}
	return clean
}

func (s *webConsoleServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "web-console",
		"version": buildVersion,
		"commit":  buildCommit,
		"date":    buildDate,
	})
}

func (s *webConsoleServer) handleNodeIDGet(w http.ResponseWriter, r *http.Request) {
	identity, ok, err := s.svc.GetIdentity(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if !ok {
		writeWebConsoleError(w, fmt.Errorf("identity is not initialized; run `aqua init`"), "initialize Aqua identity first")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, webConsoleIdentityView(identity))
}

func (s *webConsoleServer) handleNodeIDPut(w http.ResponseWriter, r *http.Request) {
	var req webConsoleSetNicknameRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	identity, err := s.svc.SetIdentityNickname(r.Context(), req.Nickname, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, webConsoleIdentityView(identity))
}

func (s *webConsoleServer) handleNodeRuntimeGet(w http.ResponseWriter, r *http.Request) {
	view, err := s.runtimeView(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, view)
}

func (s *webConsoleServer) handleNodeCardGet(w http.ResponseWriter, r *http.Request) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	identity, ok, err := s.svc.GetIdentity(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if !ok {
		writeWebConsoleError(w, fmt.Errorf("identity is not initialized; run `aqua init`"), "initialize Aqua identity first")
		return
	}
	addresses := s.node.AddrStrings()
	if len(addresses) == 0 {
		writeWebConsoleError(w, fmt.Errorf("no node addresses available"), "wait for serve to finish startup")
		return
	}
	_, raw, err := s.svc.ExportContactCard(r.Context(), addresses, aqua.ProtocolVersionV1, aqua.ProtocolVersionV1, time.Now().UTC(), nil)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Aqua-Peer-ID", identity.PeerID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *webConsoleServer) handleContactsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.ListContacts(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].PeerID < items[j].PeerID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	writeWebConsoleJSON(w, http.StatusOK, items)
}

func (s *webConsoleServer) handleContactsFetch(w http.ResponseWriter, r *http.Request) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	var req webConsoleFetchContactRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	peerID, err := extractPeerIDFromDialAddress(req.Address)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "provide a multiaddr ending with /p2p/<peer_id>")
		return
	}
	rawCard, err := s.node.GetContactCard(r.Context(), peerID, []string{strings.TrimSpace(req.Address)})
	if err != nil {
		writeWebConsoleError(w, err, "ensure the peer is reachable and running Aqua")
		return
	}
	result, err := s.svc.ImportContactCard(r.Context(), rawCard, req.DisplayName, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	contact := result.Contact
	if req.Verify {
		contact, err = s.svc.MarkContactVerified(r.Context(), contact.PeerID, time.Now().UTC())
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
	}
	fingerprint, _ := aqua.FingerprintGrouped(contact.IdentityPubEd25519)
	status := "updated"
	if result.Created {
		status = "created"
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"peer_id":     contact.PeerID,
		"node_uuid":   contact.NodeUUID,
		"nickname":    contact.Nickname,
		"trust_state": contact.TrustState,
		"verified":    req.Verify,
		"fingerprint": fingerprint,
	})
}

func (s *webConsoleServer) handleContactsImport(w http.ResponseWriter, r *http.Request) {
	var req webConsoleImportContactRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	rawCard, err := normalizeWebConsoleContactCardJSON(req.ContactCardJSON)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	result, err := s.svc.ImportContactCard(r.Context(), rawCard, req.DisplayName, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	status := "updated"
	if result.Created {
		status = "created"
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"peer_id":     result.Contact.PeerID,
		"node_uuid":   result.Contact.NodeUUID,
		"trust_state": result.Contact.TrustState,
		"conflicted":  result.Conflicted,
	})
}

func normalizeWebConsoleContactCardJSON(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("contact_card_json is required")
	}
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("decode contact_card_json string: %w", err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, fmt.Errorf("contact_card_json is required")
		}
		return []byte(text), nil
	}
	return append([]byte(nil), raw...), nil
}

func (s *webConsoleServer) handleContactShow(w http.ResponseWriter, r *http.Request) {
	peerID := strings.TrimSpace(r.PathValue("peerID"))
	contact, ok, err := s.svc.GetContactByPeerID(r.Context(), peerID)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if !ok {
		writeWebConsoleError(w, fmt.Errorf("contact not found: %s", peerID), "add the peer as a contact first")
		return
	}
	fingerprint, _ := aqua.FingerprintGrouped(contact.IdentityPubEd25519)
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"peer_id":                contact.PeerID,
		"node_uuid":              contact.NodeUUID,
		"node_id":                contact.NodeID,
		"display_name":           contact.DisplayName,
		"nickname":               contact.Nickname,
		"identity_pub_ed25519":   contact.IdentityPubEd25519,
		"fingerprint":            fingerprint,
		"addresses":              contact.Addresses,
		"trust_state":            contact.TrustState,
		"min_supported_protocol": contact.MinSupportedProtocol,
		"max_supported_protocol": contact.MaxSupportedProtocol,
		"issued_at":              contact.IssuedAt,
		"expires_at":             contact.ExpiresAt,
		"updated_at":             contact.UpdatedAt,
	})
}

func (s *webConsoleServer) handleContactDelete(w http.ResponseWriter, r *http.Request) {
	peerID := strings.TrimSpace(r.PathValue("peerID"))
	if err := s.svc.DeleteContact(r.Context(), peerID, time.Now().UTC()); err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"peer_id": peerID,
	})
}

func (s *webConsoleServer) handleContactVerify(w http.ResponseWriter, r *http.Request) {
	peerID := strings.TrimSpace(r.PathValue("peerID"))
	contact, err := s.svc.MarkContactVerified(r.Context(), peerID, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"peer_id":     contact.PeerID,
		"node_uuid":   contact.NodeUUID,
		"trust_state": contact.TrustState,
	})
}

func (s *webConsoleServer) handleInboxList(w http.ResponseWriter, r *http.Request) {
	fromPeerID := strings.TrimSpace(r.URL.Query().Get("from_peer_id"))
	topic := strings.TrimSpace(r.URL.Query().Get("topic"))
	limit, err := parseWebConsoleQueryInt(r, "limit", 50)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	unreadOnly, err := parseWebConsoleQueryBool(r, "unread", false)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	queryLimit := limit
	if unreadOnly {
		queryLimit = 0
	}
	records, err := s.svc.ListInboxMessages(r.Context(), fromPeerID, topic, queryLimit)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if unreadOnly {
		records = filterUnreadInbox(records, limit)
	}
	writeWebConsoleJSON(w, http.StatusOK, inboxRecordsForJSON(records))
}

func (s *webConsoleServer) handleInboxMarkRead(w http.ResponseWriter, r *http.Request) {
	var req webConsoleMarkReadRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	var ids []string
	if req.All {
		if len(req.MessageIDs) > 0 {
			writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "cannot combine all=true with message_ids"), "")
			return
		}
		records, err := s.svc.ListInboxMessages(r.Context(), req.FromPeerID, req.Topic, 0)
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
		ids = collectUnreadMessageIDs(records)
	} else {
		ids = normalizeInboxMessageIDs(req.MessageIDs)
		if len(ids) == 0 {
			writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "provide at least one message_id or set all=true"), "")
			return
		}
	}
	marked, err := s.svc.MarkInboxMessagesRead(r.Context(), ids, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	s.recordReadStateChange(ids, marked)
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"all":          req.All,
		"from_peer_id": strings.TrimSpace(req.FromPeerID),
		"topic":        strings.TrimSpace(req.Topic),
		"requested":    len(ids),
		"marked":       marked,
	})
}

func (s *webConsoleServer) handleOutboxList(w http.ResponseWriter, r *http.Request) {
	toPeerID := strings.TrimSpace(r.URL.Query().Get("to_peer_id"))
	topic := strings.TrimSpace(r.URL.Query().Get("topic"))
	limit, err := parseWebConsoleQueryInt(r, "limit", 50)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	records, err := s.svc.ListOutboxMessages(r.Context(), toPeerID, topic, limit)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, outboxRecordsForJSON(records))
}

func (s *webConsoleServer) handleSendDirect(w http.ResponseWriter, r *http.Request) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	var req webConsoleSendDirectRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	req.Topic = strings.TrimSpace(req.Topic)
	if req.Topic == "" {
		req.Topic = "chat.message"
	}
	req.ContentType = strings.TrimSpace(req.ContentType)
	if req.ContentType == "" {
		req.ContentType = "text/plain"
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "message is required"), "")
		return
	}
	sessionID, err := resolveOrGenerateSessionID(req.SessionID)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = messageEnvelopeKey(uuid.NewString())
	}
	result, err := s.node.PushData(r.Context(), strings.TrimSpace(req.PeerID), nil, aqua.DataPushRequest{
		Topic:          req.Topic,
		ContentType:    req.ContentType,
		PayloadBase64:  base64.RawURLEncoding.EncodeToString([]byte(message)),
		IdempotencyKey: idempotencyKey,
		SessionID:      sessionID,
		ReplyTo:        strings.TrimSpace(req.ReplyTo),
	}, req.Notification)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"peer_id":         strings.TrimSpace(req.PeerID),
		"topic":           req.Topic,
		"content_type":    req.ContentType,
		"idempotency_key": idempotencyKey,
		"session_id":      sessionID,
		"reply_to":        strings.TrimSpace(req.ReplyTo),
		"notification":    req.Notification,
		"result":          result,
	})
}

func (s *webConsoleServer) handleGroupList(w http.ResponseWriter, r *http.Request) {
	groups, err := s.svc.ListGroups(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, groupViewsFromGroups(groups))
}

func (s *webConsoleServer) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	group, err := s.svc.CreateGroup(r.Context(), time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, groupViewFromGroup(group))
}

func (s *webConsoleServer) handleGroupShow(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.PathValue("groupID"))
	details, err := s.svc.GetGroupDetails(r.Context(), groupID)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	writeWebConsoleJSON(w, http.StatusOK, groupDetailsViewFromDetails(details))
}

func (s *webConsoleServer) handleGroupInvitesList(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	incoming, err := parseWebConsoleQueryBool(r, "incoming", false)
	if err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	invites, err := s.svc.ListGroupInvites(r.Context(), groupID)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	localPeerID := ""
	if incoming {
		identity, ok, err := s.svc.GetIdentity(r.Context())
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
		if !ok {
			writeWebConsoleError(w, fmt.Errorf("identity is not initialized; run `aqua init`"), "initialize Aqua identity first")
			return
		}
		localPeerID = identity.PeerID
	}
	parsedStatus := aqua.GroupInviteStatus("")
	if status != "" {
		parsed, err := aqua.ParseGroupInviteStatus(status)
		if err != nil {
			writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
			return
		}
		parsedStatus = parsed
	}
	filtered := make([]aqua.GroupInvite, 0, len(invites))
	for _, invite := range invites {
		if parsedStatus != "" && invite.Status != parsedStatus {
			continue
		}
		if incoming && strings.TrimSpace(invite.InviteePeerID) != localPeerID {
			continue
		}
		filtered = append(filtered, invite)
	}
	writeWebConsoleJSON(w, http.StatusOK, filtered)
}

func (s *webConsoleServer) handleGroupInviteCreate(w http.ResponseWriter, r *http.Request) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("groupID"))
	var req webConsoleGroupInviteRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	invite, err := s.svc.InviteGroupMember(r.Context(), groupID, req.PeerID, time.Now().UTC())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if !req.LocalOnly {
		payloadRaw, err := aqua.EncodeGroupInviteControlMessage(invite)
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
		if err := s.pushGroupControl(r.Context(), invite.InviteePeerID, payloadRaw, groupInviteControlIdempotencyKey(invite)); err != nil {
			writeWebConsoleError(w, fmt.Errorf("invite %s stored locally but delivery failed: %w", invite.InviteID, err), "")
			return
		}
	}
	writeWebConsoleJSON(w, http.StatusOK, invite)
}

func (s *webConsoleServer) handleGroupInviteAccept(w http.ResponseWriter, r *http.Request) {
	s.handleGroupInviteDecision(w, r, aqua.GroupControlActionInviteAccept)
}

func (s *webConsoleServer) handleGroupInviteReject(w http.ResponseWriter, r *http.Request) {
	s.handleGroupInviteDecision(w, r, aqua.GroupControlActionInviteReject)
}

func (s *webConsoleServer) handleGroupInviteDecision(w http.ResponseWriter, r *http.Request, action aqua.GroupControlAction) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("groupID"))
	inviteID := strings.TrimSpace(r.PathValue("inviteID"))
	var req webConsoleGroupInviteDecisionRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	if inviteID == "" {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "invite_id is required"), "")
		return
	}
	switch action {
	case aqua.GroupControlActionInviteAccept:
		invite, group, err := s.svc.AcceptGroupInvite(r.Context(), groupID, inviteID, time.Now().UTC())
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
		if !req.LocalOnly {
			payloadRaw, err := aqua.EncodeGroupInviteDecisionMessage(invite, aqua.GroupControlActionInviteAccept, time.Now().UTC())
			if err != nil {
				writeWebConsoleError(w, err, "")
				return
			}
			if err := s.pushGroupControl(r.Context(), invite.InviterPeerID, payloadRaw, groupInviteDecisionIdempotencyKey(invite, aqua.GroupControlActionInviteAccept)); err != nil {
				writeWebConsoleError(w, fmt.Errorf("invite %s accepted locally but delivery failed: %w", invite.InviteID, err), "")
				return
			}
		}
		writeWebConsoleJSON(w, http.StatusOK, map[string]any{
			"invite": invite,
			"group":  groupViewFromGroup(group),
		})
	case aqua.GroupControlActionInviteReject:
		invite, err := s.svc.RejectGroupInvite(r.Context(), groupID, inviteID, time.Now().UTC())
		if err != nil {
			writeWebConsoleError(w, err, "")
			return
		}
		if !req.LocalOnly {
			payloadRaw, err := aqua.EncodeGroupInviteDecisionMessage(invite, aqua.GroupControlActionInviteReject, time.Now().UTC())
			if err != nil {
				writeWebConsoleError(w, err, "")
				return
			}
			if err := s.pushGroupControl(r.Context(), invite.InviterPeerID, payloadRaw, groupInviteDecisionIdempotencyKey(invite, aqua.GroupControlActionInviteReject)); err != nil {
				writeWebConsoleError(w, fmt.Errorf("invite %s rejected locally but delivery failed: %w", invite.InviteID, err), "")
				return
			}
		}
		writeWebConsoleJSON(w, http.StatusOK, invite)
	default:
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "unsupported group control action: %s", action), "")
	}
}

func (s *webConsoleServer) handleGroupMessageSend(w http.ResponseWriter, r *http.Request) {
	if s.node == nil {
		writeWebConsoleError(w, fmt.Errorf("node is not running"), "start `aqua serve` with web console enabled")
		return
	}
	groupID := strings.TrimSpace(r.PathValue("groupID"))
	var req webConsoleGroupMessageRequest
	if err := decodeWebConsoleRequest(r, &req); err != nil {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "%s", err.Error()), "")
		return
	}
	message := req.Message
	if strings.TrimSpace(message) == "" {
		writeWebConsoleError(w, aqua.WrapProtocolError(aqua.ErrInvalidParams, "message is required"), "")
		return
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "text/plain"
	}
	details, err := s.svc.GetGroupDetails(r.Context(), groupID)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if details.Group.LocalRole == "" {
		writeWebConsoleError(w, fmt.Errorf("local peer is not an active member of group %s", groupID), "")
		return
	}
	localIdentity, ok, err := s.svc.GetIdentity(r.Context())
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	if !ok {
		writeWebConsoleError(w, fmt.Errorf("identity is not initialized; run `aqua init`"), "initialize Aqua identity first")
		return
	}
	envelope := map[string]any{
		"version":        1,
		"group_id":       groupID,
		"epoch":          details.Group.Epoch,
		"sender_peer_id": localIdentity.PeerID,
		"content_type":   contentType,
		"message":        message,
	}
	payloadRaw, err := encodeJSON(envelope)
	if err != nil {
		writeWebConsoleError(w, err, "")
		return
	}
	payloadBase64 := base64.RawURLEncoding.EncodeToString(payloadRaw)
	idempotencyKey := messageEnvelopeKey("group:" + groupID + ":" + uuid.NewString())

	type sendFailure struct {
		PeerID string `json:"peer_id"`
		Error  string `json:"error"`
	}
	failures := make([]sendFailure, 0)
	attempted := 0
	sent := 0
	for _, member := range details.Members {
		peerID := strings.TrimSpace(member.PeerID)
		if peerID == "" || peerID == localIdentity.PeerID {
			continue
		}
		attempted++
		_, err := s.node.PushData(r.Context(), peerID, nil, aqua.DataPushRequest{
			Topic:          aqua.GroupMessageTopicV1,
			ContentType:    "application/json",
			PayloadBase64:  payloadBase64,
			IdempotencyKey: idempotencyKey + ":" + idempotencyToken(peerID),
		}, false)
		if err != nil {
			failures = append(failures, sendFailure{PeerID: peerID, Error: err.Error()})
			continue
		}
		sent++
	}
	writeWebConsoleJSON(w, http.StatusOK, map[string]any{
		"group_id":         groupID,
		"topic":            aqua.GroupMessageTopicV1,
		"attempted_peers":  attempted,
		"sent_peers":       sent,
		"failed_peers":     len(failures),
		"failures":         failures,
		"local_sender_id":  localIdentity.PeerID,
		"message_preview":  message,
		"payload_encoding": "json+base64url",
	})
}

func (s *webConsoleServer) pushGroupControl(ctx context.Context, peerID string, payloadRaw []byte, idempotencyKey string) error {
	if s.node == nil {
		return fmt.Errorf("node is not running")
	}
	_, err := s.node.PushData(
		ctx,
		strings.TrimSpace(peerID),
		nil,
		aqua.BuildGroupControlPushRequest(payloadRaw, idempotencyKey),
		false,
	)
	return err
}

func (s *webConsoleServer) handleEventsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	subID, ch := s.hub.subscribe()
	defer s.hub.unsubscribe(subID)

	if err := conn.WriteJSON(map[string]any{
		"event":     "web.console.ready",
		"timestamp": time.Now().UTC(),
	}); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		}
	}
}

func decodeWebConsoleRequest(r *http.Request, dst any) error {
	if r.Body == nil {
		return io.EOF
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, webConsoleReadBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}

func parseWebConsoleQueryInt(r *http.Request, key string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid query %s=%q", key, raw)
	}
	return v, nil
}

func parseWebConsoleQueryBool(r *http.Request, key string, defaultValue bool) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid query %s=%q", key, raw)
	}
}

func writeWebConsoleJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = writeJSON(w, v)
}

func writeWebConsoleError(w http.ResponseWriter, err error, suggestion string) {
	status := webConsoleHTTPStatus(err)
	code := aqua.SymbolOf(err)
	if code == "" {
		switch status {
		case http.StatusBadRequest:
			code = "BAD_REQUEST"
		case http.StatusForbidden:
			code = "FORBIDDEN"
		case http.StatusConflict:
			code = "CONFLICT"
		case http.StatusNotFound:
			code = "NOT_FOUND"
		default:
			code = "INTERNAL_ERROR"
		}
	}
	body := webConsoleErrorResponse{
		Error: webConsoleErrorBody{
			Code:       code,
			Message:    err.Error(),
			Details:    err.Error(),
			Suggestion: strings.TrimSpace(suggestion),
		},
	}
	writeWebConsoleJSON(w, status, body)
}

func webConsoleHTTPStatus(err error) int {
	switch aqua.SymbolOf(err) {
	case aqua.ErrInvalidParamsSymbol, aqua.ErrInvalidContactCardSymbol, aqua.ErrInvalidJSONProfileSymbol:
		return http.StatusBadRequest
	case aqua.ErrUnauthorizedSymbol, aqua.ErrPeerIDMismatchSymbol:
		return http.StatusForbidden
	case aqua.ErrContactConflictedSymbol:
		return http.StatusConflict
	case aqua.ErrPayloadTooLargeSymbol:
		return http.StatusRequestEntityTooLarge
	case aqua.ErrRateLimitedSymbol:
		return http.StatusTooManyRequests
	case aqua.ErrBusySymbol:
		return http.StatusServiceUnavailable
	case aqua.ErrMethodNotAllowedSymbol:
		return http.StatusMethodNotAllowed
	default:
		msg := strings.ToLower(strings.TrimSpace(err.Error()))
		switch {
		case strings.Contains(msg, "not found"):
			return http.StatusNotFound
		case strings.Contains(msg, "required"), strings.Contains(msg, "invalid"):
			return http.StatusBadRequest
		default:
			return http.StatusInternalServerError
		}
	}
}

func webConsoleIdentityView(identity aqua.Identity) map[string]any {
	fingerprint, _ := aqua.FingerprintGrouped(identity.IdentityPubEd25519)
	return map[string]any{
		"node_uuid":            identity.NodeUUID,
		"peer_id":              identity.PeerID,
		"node_id":              identity.NodeID,
		"nickname":             identity.Nickname,
		"identity_pub_ed25519": identity.IdentityPubEd25519,
		"fingerprint":          fingerprint,
		"created_at":           identity.CreatedAt,
		"updated_at":           identity.UpdatedAt,
	}
}

func webConsoleDataPushEventView(event aqua.DataPushEvent) map[string]any {
	payloadText := ""
	if payloadIsTextual(event.ContentType) {
		payloadText = string(event.PayloadBytes)
	}
	return map[string]any{
		"event":           "agent.data.push",
		"from_peer_id":    event.FromPeerID,
		"topic":           event.Topic,
		"content_type":    event.ContentType,
		"idempotency_key": event.IdempotencyKey,
		"session_id":      event.SessionID,
		"reply_to":        event.ReplyTo,
		"deduped":         event.Deduped,
		"received_at":     event.ReceivedAt,
		"payload_text":    payloadText,
		"payload_base64":  event.PayloadBase64,
	}
}

func webConsoleRelayEventView(event aqua.RelayEvent) map[string]any {
	return map[string]any{
		"event":          event.Event,
		"path":           event.Path,
		"relay_peer_id":  event.RelayPeerID,
		"target_peer_id": event.TargetPeerID,
		"reason":         event.Reason,
		"timestamp":      event.Timestamp,
	}
}

package maep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var allowedMethodsV1 = []string{
	"agent.ping",
	"agent.capabilities.get",
	"agent.data.push",
}

var rpcErrorCodeBySymbol = map[string]int{
	ErrUnauthorizedSymbol:        -32001,
	ErrPeerIDMismatchSymbol:      -32002,
	ErrContactConflictedSymbol:   -32003,
	ErrMethodNotAllowedSymbol:    -32004,
	ErrPayloadTooLargeSymbol:     -32005,
	ErrRateLimitedSymbol:         -32006,
	ErrUnsupportedProtocolSymbol: -32007,
	ErrInvalidJSONProfileSymbol:  -32008,
	ErrInvalidContactCardSymbol:  -32009,
	ErrInvalidParamsSymbol:       -32602,
}

type rpcRequest struct {
	JSONRPC string
	ID      any
	HasID   bool
	Method  string
	Params  json.RawMessage
}

type rpcErrorObject struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorObject `json:"error,omitempty"`
}

type rpcDataPushParams struct {
	Topic          string `json:"topic"`
	ContentType    string `json:"content_type"`
	PayloadBase64  string `json:"payload_base64"`
	IdempotencyKey string `json:"idempotency_key"`
}

type rpcDataPushResult struct {
	Accepted bool `json:"accepted"`
	Deduped  bool `json:"deduped"`
}

func parseRPCRequest(raw []byte) (rpcRequest, error) {
	var obj map[string]any
	if err := decodeStrictJSON(raw, &obj); err != nil {
		return rpcRequest{}, err
	}

	jsonrpcValue, ok := obj["jsonrpc"]
	if !ok {
		return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "jsonrpc is required")
	}
	jsonrpcString, ok := jsonrpcValue.(string)
	if !ok || strings.TrimSpace(jsonrpcString) != JSONRPCVersion {
		return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "jsonrpc must be %q", JSONRPCVersion)
	}

	methodValue, ok := obj["method"]
	if !ok {
		return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "method is required")
	}
	methodString, ok := methodValue.(string)
	if !ok || strings.TrimSpace(methodString) == "" {
		return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "method must be a non-empty string")
	}

	paramsRaw := json.RawMessage([]byte("{}"))
	if paramsValue, ok := obj["params"]; ok {
		rawBytes, err := json.Marshal(paramsValue)
		if err != nil {
			return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "params marshal failed: %v", err)
		}
		paramsRaw = rawBytes
	}

	req := rpcRequest{
		JSONRPC: jsonrpcString,
		Method:  strings.TrimSpace(methodString),
		Params:  paramsRaw,
	}

	if idValue, ok := obj["id"]; ok {
		if !isValidRPCID(idValue) {
			return rpcRequest{}, WrapProtocolError(ErrInvalidParams, "id must be string or integer")
		}
		req.HasID = true
		req.ID = idValue
	}
	return req, nil
}

func isValidRPCID(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) != ""
	case json.Number:
		return isStrictInteger(x.String())
	default:
		return false
	}
}

func decodeRPCParams(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return WrapProtocolError(ErrInvalidParams, "params is required")
	}
	if err := decodeStrictJSON(raw, out); err != nil {
		return WrapProtocolError(ErrInvalidParams, "params decode failed: %v", err)
	}
	return nil
}

func makeRPCSuccess(id any, result any) ([]byte, error) {
	resp := rpcResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
	return json.Marshal(resp)
}

func makeRPCError(id any, symbol string, details string) ([]byte, error) {
	code, ok := rpcErrorCodeBySymbol[symbol]
	if !ok {
		code = -32000
	}
	errObj := &rpcErrorObject{
		Code:    code,
		Message: symbol,
	}
	if strings.TrimSpace(details) != "" {
		errObj.Data = map[string]any{"details": details}
	}
	resp := rpcResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   errObj,
	}
	return json.Marshal(resp)
}

func parseRPCResponse(raw []byte) (json.RawMessage, string, string, error) {
	var obj map[string]any
	if err := decodeStrictJSON(raw, &obj); err != nil {
		return nil, "", "", err
	}

	jsonrpcValue, ok := obj["jsonrpc"]
	if !ok {
		return nil, "", "", fmt.Errorf("response missing jsonrpc")
	}
	jsonrpcString, ok := jsonrpcValue.(string)
	if !ok || strings.TrimSpace(jsonrpcString) != JSONRPCVersion {
		return nil, "", "", fmt.Errorf("response jsonrpc must be %q", JSONRPCVersion)
	}

	if errValue, ok := obj["error"]; ok {
		errMap, ok := errValue.(map[string]any)
		if !ok {
			return nil, "", "", fmt.Errorf("response error must be object")
		}
		message, _ := errMap["message"].(string)
		details := ""
		if dataValue, ok := errMap["data"].(map[string]any); ok {
			if detailsValue, ok := dataValue["details"].(string); ok {
				details = detailsValue
			}
		}
		return nil, strings.TrimSpace(message), details, nil
	}

	resultValue, ok := obj["result"]
	if !ok {
		return nil, "", "", fmt.Errorf("response missing result")
	}
	resultRaw, err := json.Marshal(resultValue)
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal response result: %w", err)
	}
	return resultRaw, "", "", nil
}

func generateRPCRequestID() string {
	return "req_" + uuid.NewString()
}

func isAllowedMethod(method string) bool {
	for _, candidate := range allowedMethodsV1 {
		if method == candidate {
			return true
		}
	}
	return false
}

// Best-effort extraction of request id for parse-error responses.
// This intentionally uses non-strict JSON decode so we can still reply when
// semantic validation fails but a valid id is present.
func extractRPCIDForError(raw []byte) (any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, false
	}
	idValue, ok := obj["id"]
	if !ok || !isValidRPCID(idValue) {
		return nil, false
	}
	return idValue, true
}

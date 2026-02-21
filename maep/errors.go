package maep

import (
	"errors"
	"fmt"
)

const (
	ErrUnauthorizedSymbol        = "ERR_UNAUTHORIZED"
	ErrPeerIDMismatchSymbol      = "ERR_PEER_ID_MISMATCH"
	ErrContactConflictedSymbol   = "ERR_CONTACT_CONFLICTED"
	ErrMethodNotAllowedSymbol    = "ERR_METHOD_NOT_ALLOWED"
	ErrPayloadTooLargeSymbol     = "ERR_PAYLOAD_TOO_LARGE"
	ErrRateLimitedSymbol         = "ERR_RATE_LIMITED"
	ErrUnsupportedProtocolSymbol = "ERR_UNSUPPORTED_PROTOCOL"
	ErrInvalidJSONProfileSymbol  = "ERR_INVALID_JSON_PROFILE"
	ErrInvalidContactCardSymbol  = "ERR_INVALID_CONTACT_CARD"
	ErrInvalidParamsSymbol       = "ERR_INVALID_PARAMS"
)

type ProtocolError struct {
	Symbol  string
	Message string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Symbol
	}
	return fmt.Sprintf("%s: %s", e.Symbol, e.Message)
}

func (e *ProtocolError) Is(target error) bool {
	t, ok := target.(*ProtocolError)
	if !ok {
		return false
	}
	return e.Symbol == t.Symbol
}

var (
	ErrUnauthorized        = &ProtocolError{Symbol: ErrUnauthorizedSymbol, Message: "unauthorized"}
	ErrPeerIDMismatch      = &ProtocolError{Symbol: ErrPeerIDMismatchSymbol, Message: "peer id mismatch"}
	ErrInvalidJSONProfile  = &ProtocolError{Symbol: ErrInvalidJSONProfileSymbol, Message: "invalid JSON profile"}
	ErrInvalidContactCard  = &ProtocolError{Symbol: ErrInvalidContactCardSymbol, Message: "invalid contact card"}
	ErrContactConflicted   = &ProtocolError{Symbol: ErrContactConflictedSymbol, Message: "contact conflicted"}
	ErrMethodNotAllowed    = &ProtocolError{Symbol: ErrMethodNotAllowedSymbol, Message: "method not allowed"}
	ErrPayloadTooLarge     = &ProtocolError{Symbol: ErrPayloadTooLargeSymbol, Message: "payload too large"}
	ErrRateLimited         = &ProtocolError{Symbol: ErrRateLimitedSymbol, Message: "rate limited"}
	ErrUnsupportedProtocol = &ProtocolError{Symbol: ErrUnsupportedProtocolSymbol, Message: "unsupported protocol"}
	ErrInvalidParams       = &ProtocolError{Symbol: ErrInvalidParamsSymbol, Message: "invalid params"}
)

func WrapProtocolError(base *ProtocolError, format string, args ...any) error {
	if base == nil {
		return fmt.Errorf(format, args...)
	}
	msg := fmt.Sprintf(format, args...)
	return &ProtocolError{Symbol: base.Symbol, Message: msg}
}

func SymbolOf(err error) string {
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return pe.Symbol
	}
	return ""
}

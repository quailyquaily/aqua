package maep

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var jsonIntegerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func ValidateStrictJSONProfile(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return WrapProtocolError(ErrInvalidJSONProfile, "empty JSON")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := validateJSONValue(dec); err != nil {
		return err
	}

	_, err := dec.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return WrapProtocolError(ErrInvalidJSONProfile, "invalid trailing token: %v", err)
	}
	return WrapProtocolError(ErrInvalidJSONProfile, "trailing JSON value is not allowed")
}

func validateJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		if err == io.EOF {
			return WrapProtocolError(ErrInvalidJSONProfile, "unexpected end of input")
		}
		return WrapProtocolError(ErrInvalidJSONProfile, "token decode failed: %v", err)
	}
	return validateTokenValue(dec, tok)
}

func validateTokenValue(dec *json.Decoder, tok json.Token) error {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return validateJSONObject(dec)
		case '[':
			return validateJSONArray(dec)
		default:
			return WrapProtocolError(ErrInvalidJSONProfile, "unexpected delimiter %q", string(t))
		}
	case nil:
		return WrapProtocolError(ErrInvalidJSONProfile, "null is not allowed")
	case json.Number:
		if !isStrictInteger(t.String()) {
			return WrapProtocolError(ErrInvalidJSONProfile, "float number is not allowed: %s", t.String())
		}
		return nil
	case string, bool:
		return nil
	default:
		return WrapProtocolError(ErrInvalidJSONProfile, "unsupported token type %T", tok)
	}
}

func validateJSONObject(dec *json.Decoder) error {
	seen := map[string]struct{}{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return WrapProtocolError(ErrInvalidJSONProfile, "failed to read object key: %v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return WrapProtocolError(ErrInvalidJSONProfile, "object key must be string")
		}
		if _, exists := seen[key]; exists {
			return WrapProtocolError(ErrInvalidJSONProfile, "duplicate object key: %q", key)
		}
		seen[key] = struct{}{}

		if err := validateJSONValue(dec); err != nil {
			return err
		}
	}
	endTok, err := dec.Token()
	if err != nil {
		return WrapProtocolError(ErrInvalidJSONProfile, "object not closed: %v", err)
	}
	delim, ok := endTok.(json.Delim)
	if !ok || delim != '}' {
		return WrapProtocolError(ErrInvalidJSONProfile, "object close delimiter is invalid")
	}
	return nil
}

func validateJSONArray(dec *json.Decoder) error {
	for dec.More() {
		if err := validateJSONValue(dec); err != nil {
			return err
		}
	}
	endTok, err := dec.Token()
	if err != nil {
		return WrapProtocolError(ErrInvalidJSONProfile, "array not closed: %v", err)
	}
	delim, ok := endTok.(json.Delim)
	if !ok || delim != ']' {
		return WrapProtocolError(ErrInvalidJSONProfile, "array close delimiter is invalid")
	}
	return nil
}

func isStrictInteger(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.ContainsAny(raw, ".eE") {
		return false
	}
	return jsonIntegerPattern.MatchString(raw)
}

func decodeStrictJSON(data []byte, out any) error {
	if err := ValidateStrictJSONProfile(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return WrapProtocolError(ErrInvalidJSONProfile, "JSON decode failed: %v", err)
	}
	if dec.More() {
		return WrapProtocolError(ErrInvalidJSONProfile, "trailing JSON input is not allowed")
	}
	return nil
}

func mustJSONObject(raw []byte) error {
	var probe any
	if err := decodeStrictJSON(raw, &probe); err != nil {
		return err
	}
	if _, ok := probe.(map[string]any); !ok {
		return WrapProtocolError(ErrInvalidJSONProfile, "top-level JSON value must be an object")
	}
	return nil
}

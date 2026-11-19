package prettylog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// MaskedValue never stores a raw, printable value.
type MaskedValue struct{ safe string }

func (m MaskedValue) String() string               { return m.safe }
func (m MaskedValue) MarshalJSON() ([]byte, error) { return json.Marshal(m.safe) }

// MaskedJSON stores only redacted JSON; its MarshalJSON preserves object structure.
type MaskedJSON struct{ safe any }

func (m MaskedJSON) MarshalJSON() ([]byte, error) { return json.Marshal(m.safe) }
func (m MaskedJSON) String() string {
	b, err := json.Marshal(m.safe)
	if err != nil {
		return `[invalid json]`
	}
	return string(b)
}

func maskString(s string, cfg MaskOptions) string {
	chars := []rune(s)
	start, end := cfg.VisibleStart, cfg.VisibleEnd
	if start > len(chars) {
		start = len(chars)
	}
	if end > len(chars)-start {
		end = len(chars) - start
	}
	maskCount := len(chars) - start - end
	if maskCount <= 0 {
		// Never reveal a short secret just because it is shorter than VisibleEnd.
		return strings.Repeat(string(cfg.Character), len(chars))
	}
	return string(chars[:start]) + strings.Repeat(string(cfg.Character), maskCount) + string(chars[len(chars)-end:])
}

// Mask uses the constructor default, e.g. "1234567890" => "******7890".
func (l *Logger) Mask(v any) MaskedValue {
	return MaskedValue{safe: maskString(fmt.Sprint(v), l.maskConfig())}
}

// MaskKeepLast overrides the number of visible trailing Unicode characters.
func (l *Logger) MaskKeepLast(v any, visible int) MaskedValue {
	cfg := l.maskConfig()
	if visible < 0 {
		visible = 0
	}
	cfg.VisibleStart, cfg.VisibleEnd = 0, visible
	return MaskedValue{safe: maskString(fmt.Sprint(v), cfg)}
}

// MaskJSON accepts a JSON string, []byte, or any JSON-serializable Go value.
// It recursively masks sensitive keys, including those within arrays.
// Invalid input is replaced by a safe placeholder (never raw content).
func (l *Logger) MaskJSON(input any) MaskedJSON {
	var data []byte
	switch v := input.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(input)
		if err != nil {
			return MaskedJSON{safe: "[unserializable json]"}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return MaskedJSON{safe: "[invalid json]"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return MaskedJSON{safe: "[invalid json]"}
	}
	return MaskedJSON{safe: redactJSON(value, l.maskConfig())}
}

func redactJSON(value any, cfg MaskOptions) any {
	switch v := value.(type) {
	case map[string]any:
		safe := make(map[string]any, len(v))
		for k, child := range v {
			if isSensitiveKey(k, cfg) {
				safe[k] = maskString(fmt.Sprint(child), cfg)
			} else {
				safe[k] = redactJSON(child, cfg)
			}
		}
		return safe
	case []any:
		safe := make([]any, len(v))
		for i, child := range v {
			safe[i] = redactJSON(child, cfg)
		}
		return safe
	default:
		return value
	}
}

func isSensitiveKey(key string, cfg MaskOptions) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	// Exact match by design to avoid unexpected masking of unrelated attributes.
	keys := []string{"password", "passwd", "pwd", "token", "access_token",
		"refresh_token", "api_key", "apikey", "authorization", "secret", "private_key"}
	keys = append(keys, cfg.SensitiveKeys...)
	for _, sensitive := range keys {
		if k == strings.ToLower(strings.TrimSpace(sensitive)) {
			return true
		}
	}
	return false
}

func redactField(key string, value any, cfg MaskOptions, autoMask bool) any {
	if autoMask && isSensitiveKey(key, cfg) {
		return MaskedValue{safe: maskString(fieldString(value), cfg)}
	}
	switch v := value.(type) {
	case error:
		return v.Error()
	default:
		return value
	}
}

func (l *Logger) maskConfig() MaskOptions {
	l.shared.mu.RLock()
	defer l.shared.mu.RUnlock()
	return l.shared.opts.Mask
}

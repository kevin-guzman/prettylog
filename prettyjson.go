package prettylog

import (
	"bytes"
	"encoding/json"
)

// PrettyJSONValue is an immutable JSON snapshot. It renders indented JSON in
// text formatters and preserves the original JSON structure in JSONFormatter.
type PrettyJSONValue struct {
	compact []byte
	pretty  string
}

func (v PrettyJSONValue) String() string { return v.pretty }

func (v PrettyJSONValue) MarshalJSON() ([]byte, error) {
	if len(v.compact) == 0 {
		return []byte("null"), nil
	}
	return append([]byte(nil), v.compact...), nil
}

func (v PrettyJSONValue) prettyJSONText() string { return v.pretty }

// PrettyJSON accepts a JSON string, []byte, json.RawMessage, or any value
// supported by encoding/json. Invalid values are replaced by a safe marker.
func (l *Logger) PrettyJSON(input any) PrettyJSONValue {
	compact, marker := compactJSON(input)
	if marker != "" {
		return PrettyJSONValue{compact: []byte("null"), pretty: marker}
	}

	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return PrettyJSONValue{compact: []byte("null"), pretty: "[invalid json]"}
	}
	return PrettyJSONValue{compact: compact, pretty: out.String()}
}

func compactJSON(input any) ([]byte, string) {
	var data []byte
	switch value := input.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	case json.RawMessage:
		data = value
	default:
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, "[unserializable json]"
		}
		data = encoded
	}

	if !json.Valid(data) {
		return nil, "[invalid json]"
	}
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return nil, "[invalid json]"
	}
	return append([]byte(nil), out.Bytes()...), ""
}

type prettyJSONText interface {
	prettyJSONText() string
}

func getPrettyJSONText(value any) (string, bool) {
	pretty, ok := value.(prettyJSONText)
	if !ok {
		return "", false
	}
	return pretty.prettyJSONText(), true
}

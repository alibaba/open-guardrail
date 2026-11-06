// Package run owns validation and content identity for immutable Run inputs.
package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"unicode/utf8"
)

const (
	MaxChangeSetBytes = 256 << 10
	maxJSONDepth      = 32
	maxJSONNodes      = 16384
)

var ErrInvalidChangeSet = errors.New("changeSet must be a valid JSON object")

type object map[string]any

// Canonicalize accepts arbitrary business fields without interpreting them.
// Object keys are sorted, insignificant whitespace is removed and number
// lexemes are preserved. Duplicate keys are rejected so one payload cannot
// have multiple interpretations.
func Canonicalize(raw []byte) ([]byte, string, error) {
	if len(raw) == 0 || len(raw) > MaxChangeSetBytes || !utf8.Valid(raw) {
		return nil, "", ErrInvalidChangeSet
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodes := 0
	value, err := decodeValue(decoder, 0, &nodes)
	if err != nil {
		return nil, "", ErrInvalidChangeSet
	}
	if _, ok := value.(object); !ok {
		return nil, "", ErrInvalidChangeSet
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, "", ErrInvalidChangeSet
	}
	canonical := appendCanonical(nil, value)
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}

func decodeValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	(*nodes)++
	if depth > maxJSONDepth || *nodes > maxJSONNodes {
		return nil, ErrInvalidChangeSet
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		switch token.(type) {
		case nil, bool, string, json.Number:
			return token, nil
		default:
			return nil, ErrInvalidChangeSet
		}
	}
	switch delim {
	case '{':
		result := object{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, ErrInvalidChangeSet
			}
			if _, duplicate := result[key]; duplicate {
				return nil, ErrInvalidChangeSet
			}
			value, err := decodeValue(decoder, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, ErrInvalidChangeSet
		}
		return result, nil
	case '[':
		result := []any{}
		for decoder.More() {
			value, err := decodeValue(decoder, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, ErrInvalidChangeSet
		}
		return result, nil
	default:
		return nil, ErrInvalidChangeSet
	}
}

func appendCanonical(out []byte, value any) []byte {
	switch v := value.(type) {
	case object:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out = append(out, '{')
		for i, key := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			encoded, _ := json.Marshal(key)
			out = append(out, encoded...)
			out = append(out, ':')
			out = appendCanonical(out, v[key])
		}
		return append(out, '}')
	case []any:
		out = append(out, '[')
		for i, item := range v {
			if i > 0 {
				out = append(out, ',')
			}
			out = appendCanonical(out, item)
		}
		return append(out, ']')
	case string:
		encoded, _ := json.Marshal(v)
		return append(out, encoded...)
	case json.Number:
		return append(out, v.String()...)
	case bool:
		if v {
			return append(out, "true"...)
		}
		return append(out, "false"...)
	case nil:
		return append(out, "null"...)
	default:
		panic("unexpected JSON value")
	}
}

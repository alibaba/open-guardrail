package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

// Match encoding/json's document depth limit while traversing tokens.
const maxRequestJSONDepth = 10000

// decodeJSON accepts one unambiguous JSON object. Endpoints own their limits
// and error mapping; malformed UTF-8 or duplicate keys must not be normalized.
func decodeJSON(w http.ResponseWriter, r *http.Request, value any, limit int64, required ...string) bool {
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	raw, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(raw) {
		return false
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if token, err := tokens.Token(); err != nil || token != json.Delim('{') {
		return false
	}
	if !uniqueJSONObject(tokens, 1) {
		return false
	}
	if _, err := tokens.Token(); err != io.EOF {
		return false
	}
	if len(required) > 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return false
		}
		for _, key := range required {
			value, ok := fields[key]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil
}

func uniqueJSONObject(decoder *json.Decoder, depth int) bool {
	if depth > maxRequestJSONDepth {
		return false
	}
	keys := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return false
		}
		if _, duplicate := keys[key]; duplicate {
			return false
		}
		keys[key] = struct{}{}
		if !uniqueJSONValue(decoder, depth+1) {
			return false
		}
	}
	end, err := decoder.Token()
	return err == nil && end == json.Delim('}')
}

func uniqueJSONValue(decoder *json.Decoder, depth int) bool {
	if depth > maxRequestJSONDepth {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	switch token {
	case json.Delim('{'):
		return uniqueJSONObject(decoder, depth)
	case json.Delim('['):
		for decoder.More() {
			if !uniqueJSONValue(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		_, delimiter := token.(json.Delim)
		return !delimiter
	}
}

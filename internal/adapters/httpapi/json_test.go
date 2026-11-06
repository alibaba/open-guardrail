package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		limit                   int64
		want                    bool
	}{
		{"object", "application/json", `{"name":"ok"}`, 100, true},
		{"charset and whitespace", "application/json; charset=utf-8", "{\"name\":\"ok\"} \n", 100, true},
		{"wrong content type", "text/plain", `{}`, 100, false},
		{"unknown field", "application/json", `{"other":true}`, 100, false},
		{"second document", "application/json", `{} {}`, 100, false},
		{"trailing garbage", "application/json", `{} trailing`, 100, false},
		{"empty", "application/json", "", 100, false},
		{"truncated", "application/json", `{"name":`, 100, false},
		{"null", "application/json", `null`, 100, false},
		{"array", "application/json", `[]`, 100, false},
		{"duplicate field", "application/json", `{"name":"first","name":"second"}`, 100, false},
		{"escaped duplicate field", "application/json", `{"name":"first","na\u006de":"second"}`, 100, false},
		{"invalid UTF-8", "application/json", "{\"name\":\"\xff\"}", 100, false},
		{"exact limit", "application/json", `{}`, 2, true},
		{"over limit", "application/json", `{} `, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			var input struct {
				Name string `json:"name"`
			}
			if got := decodeJSON(httptest.NewRecorder(), r, &input, tc.limit); got != tc.want {
				t.Fatalf("accepted=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestDecodeJSONRejectsNestedDuplicateFields(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"value":{"a":1,"a":2}}`, false},
		{`{"value":[{"a":1,"a":2}]}`, false},
		{`{"value":{"a":1},"other":{"a":2}}`, true},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		var input map[string]any
		if got := decodeJSON(httptest.NewRecorder(), r, &input, 1024); got != tc.want {
			t.Fatalf("body=%s accepted=%v want=%v", tc.body, got, tc.want)
		}
	}
}

func TestDecodeJSONRejectsExcessiveDepth(t *testing.T) {
	body := `{"value":` + strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001) + "}"
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	var input map[string]any
	if decodeJSON(httptest.NewRecorder(), r, &input, int64(len(body))) {
		t.Fatal("accepted excessive JSON depth")
	}
}

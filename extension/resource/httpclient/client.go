// Package httpclient calls optional ChangeSet and binding candidate plugins.
package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alibaba/open-guardrail/extension/resource"
)

const maxBytes = 512 << 10

var errInvalidArgument = errors.New("plugin request is invalid")
var errUnavailable = errors.New("resource plugin unavailable or invalid response")

var transport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 500 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext
	return transport
}()

type Client struct {
	endpoint string
	http     *http.Client
}

// New permits plaintext only on a literal loopback address for local contract
// demonstrations. Remote deployments require HTTPS.
func New(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.ContainsAny(endpoint, "?#") {
		return nil, errors.New("invalid resource plugin configuration")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("resource plugin requires HTTPS outside loopback")
		}
	}
	return &Client{
		endpoint: endpoint,
		http: &http.Client{Transport: transport, Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) invoke(ctx context.Context, operation string, request any) (json.RawMessage, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errUnavailable
	}
	id := hex.EncodeToString(nonce[:])
	envelope := struct {
		SPIVersion string `json:"spiVersion"`
		RequestID  string `json:"requestId"`
		Operation  string `json:"operation"`
		Payload    any    `json:"payload"`
	}{resource.SPIVersion, id, operation, request}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, errUnavailable
	}
	data, err := c.call(ctx, body)
	if err != nil {
		return nil, err
	}
	var response struct {
		SPIVersion string          `json:"spiVersion"`
		RequestID  string          `json:"requestId"`
		Result     json.RawMessage `json:"result,omitempty"`
		Error      *struct {
			Code              string `json:"code"`
			Message           string `json:"message"`
			RetryAfterSeconds *int   `json:"retryAfterSeconds,omitempty"`
		} `json:"error,omitempty"`
	}
	if !exactKeys(data, []string{"spiVersion", "requestId"}, "result", "error") ||
		strictJSON(data, &response) != nil || response.SPIVersion != resource.SPIVersion ||
		response.RequestID != id ||
		((len(response.Result) == 0) == (response.Error == nil)) {
		return nil, errUnavailable
	}
	if response.Error != nil {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		if !exactKeys(fields["error"], []string{"code", "message"}, "retryAfterSeconds") {
			return nil, errUnavailable
		}
		if response.Error.Message == "" || len(response.Error.Message) > 256 ||
			(response.Error.RetryAfterSeconds != nil && *response.Error.RetryAfterSeconds < 0) {
			return nil, errUnavailable
		}
		if response.Error.Code == "INVALID_ARGUMENT" {
			return nil, errInvalidArgument
		}
		return nil, errUnavailable
	}
	return response.Result, nil
}

func (c *Client) call(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, transportUnavailable(err)
	}
	defer response.Body.Close()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK {
		return nil, unavailable(fmt.Sprintf("http_status_%d", response.StatusCode))
	}
	if err != nil || media != "application/json" {
		return nil, unavailable("response_content_type")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, unavailable("response_read")
	}
	if len(data) > maxBytes {
		return nil, unavailable("response_too_large")
	}
	return data, nil
}

// Classify failures without retaining driver text, URLs, headers or bodies.
// Startup logs can distinguish transport failures from protocol rejection.
func unavailable(reason string) error {
	return fmt.Errorf("%w: %s", errUnavailable, reason)
}

func transportUnavailable(err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsTimeout {
			return unavailable("dns_timeout")
		}
		return unavailable("dns_error")
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return unavailable("tls_certificate")
	}
	if errors.Is(err, context.Canceled) {
		return unavailable("request_canceled")
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return unavailable("transport_timeout")
	}
	return unavailable("transport_error")
}

// strictJSON rejects unknown fields, duplicate keys, trailing documents,
// invalid UTF-8 and deep responses before ordinary typed decoding.
func strictJSON(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errUnavailable
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	if err := jsonValue(tokens, 0); err != nil {
		return err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return errUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return errUnavailable
	}
	return nil
}

// encoding/json matches struct fields case-insensitively. Require the exact
// protocol names as well, rather than accepting an accidental schema variant.
func exactKeys(data []byte, required []string, optional ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return false
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) ||
			(!slices.Contains(required, name) && !slices.Contains(optional, name)) {
			return false
		}
	}
	return true
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errUnavailable
	}
	token, err := d.Token()
	if err != nil {
		return errUnavailable
	}
	switch token {
	case json.Delim('{'):
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || keys[name] {
				return errUnavailable
			}
			keys[name] = true
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return errUnavailable
		}
	case json.Delim('['):
		for d.More() {
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return errUnavailable
		}
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return errUnavailable
		}
	}
	return nil
}

package ctlcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/buildinfo"
	"golang.org/x/mod/semver"
)

const apiVersion = "v1alpha1"

func defaultServerURL() string {
	if configured := os.Getenv("OPEN_GUARDRAIL_URL"); configured != "" {
		return configured
	}
	return "http://127.0.0.1:8080"
}

type apiClient struct {
	generated        *contract.ClientWithResponses
	maxResponseBytes int64
}

type response interface {
	GetBody() []byte
	StatusCode() int
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func newAPIClient(server, token string, timeout time.Duration, maxResponseBytes int64, allowLoopbackHTTP bool) (*apiClient, error) {
	origin, err := parseServer(server)
	if err != nil {
		return nil, usageError(err.Error())
	}
	if token != "" && origin.Scheme != "https" {
		loopback := net.ParseIP(origin.Hostname())
		if !allowLoopbackHTTP || loopback == nil || !loopback.IsLoopback() {
			return nil, usageError("API tokens require HTTPS except for literal loopback addresses")
		}
	}
	httpClient := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: responseLimitTransport{
			base:  http.DefaultTransport,
			limit: maxResponseBytes,
		},
	}
	generated, err := contract.NewClientWithResponses(origin.String(),
		contract.WithHTTPClient(httpClient),
		contract.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			if token != "" {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			return nil
		}),
	)
	if err != nil {
		return nil, usageError("cannot construct API client")
	}
	return &apiClient{generated: generated, maxResponseBytes: maxResponseBytes}, nil
}

type responseLimitTransport struct {
	base  http.RoundTripper
	limit int64
}

func (t responseLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = limitedReadCloser{
		Reader: io.LimitReader(response.Body, t.limit+1),
		Closer: response.Body,
	}
	return response, nil
}

func parseServer(server string) (*url.URL, error) {
	origin, err := url.Parse(server)
	if err != nil || origin == nil || origin.Host == "" ||
		(origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil ||
		origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("server must be an HTTP(S) URL without credentials, query or fragment")
	}
	return origin, nil
}

func validateResponse(result response, maxResponseBytes int64, operation string, uncertain bool) error {
	if result == nil || result.StatusCode() == 0 || int64(len(result.GetBody())) > maxResponseBytes {
		return internalError("invalid server response")
	}
	if result.StatusCode() < http.StatusMultipleChoices {
		return nil
	}
	var body contract.Error
	_ = json.Unmarshal(result.GetBody(), &body)
	kind := exitServerRejected
	if result.StatusCode() == http.StatusUnauthorized || result.StatusCode() == http.StatusForbidden {
		kind = exitAuthentication
	}
	message := fmt.Sprintf("%s failed (HTTP %d)", operation, result.StatusCode())
	if body.Code != "" {
		message += ": " + body.Code
	}
	if body.TraceId != "" {
		message += " (trace " + body.TraceId + ")"
	}
	if uncertain {
		message += "; check server state before retrying"
	}
	return &commandError{kind: kind, message: message}
}

func transportError(err error, uncertain bool) error {
	if err == nil {
		return nil
	}
	message := "cannot reach OpenGuardrail server"
	if uncertain {
		message = "request failed; state may have changed; check server state before retrying"
	}
	return &commandError{kind: exitConnection, message: message}
}

func (c *apiClient) ensureCompatible(ctx context.Context) error {
	result, err := c.generated.GetCapabilitiesWithResponse(ctx)
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(result, c.maxResponseBytes, "capabilities request", false); err != nil {
		return err
	}
	if result.JSON200 == nil {
		return internalError("invalid capabilities response")
	}
	compatibleAPI := false
	for _, version := range result.JSON200.ApiVersions {
		if version == apiVersion {
			compatibleAPI = true
			break
		}
	}
	if !compatibleAPI {
		return compatibilityError("server does not support API " + apiVersion)
	}
	version := buildinfo.Version
	if version == "" || version == "dev" {
		return nil
	}
	current := "v" + strings.TrimPrefix(version, "v")
	minimum := "v" + strings.TrimPrefix(result.JSON200.Cli.MinimumVersion, "v")
	maximum := "v" + strings.TrimPrefix(result.JSON200.Cli.MaximumVersionExclusive, "v")
	if !semver.IsValid(current) || !semver.IsValid(minimum) || !semver.IsValid(maximum) {
		return compatibilityError("server returned an invalid CLI compatibility range")
	}
	if semver.Compare(current, minimum) < 0 || semver.Compare(current, maximum) >= 0 {
		return compatibilityError(fmt.Sprintf("guardrailctl %s is outside server range [%s, %s)", version,
			result.JSON200.Cli.MinimumVersion, result.JSON200.Cli.MaximumVersionExclusive))
	}
	return nil
}

// newCompatibleClient negotiates anonymously before attaching any credential.
func newCompatibleClient(server, token string, responseLimit int64) (*apiClient, error) {
	client, err := newAPIClient(server, "", 15*time.Second, responseLimit, true)
	if err != nil {
		return nil, err
	}
	if err = client.ensureCompatible(context.Background()); err != nil {
		return nil, err
	}
	if token == "" {
		return client, nil
	}
	return newAPIClient(server, token, 15*time.Second, responseLimit, true)
}

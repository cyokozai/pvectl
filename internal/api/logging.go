package api

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"time"

	"github.com/cyokozai/pvectl/internal/verbose"
)

// bodyPreviewLimit bounds how much of a body -v=8 prints. -v=9 prints
// the whole thing. The limit exists so that a 200 KB /cluster/resources
// answer does not bury the request that came after it.
const bodyPreviewLimit = 1024

// loggingTransport logs the HTTP conversation at -v=6 and above.
//
// pvectl does not use the SDK's own debug switch. That switch dumps the
// request with httputil.DumpRequestOut, which prints the Authorization
// header verbatim — the API token itself — with no way to mask it, no
// level control, and no choice of destination. Owning the transport
// gives all three.
type loggingTransport struct {
	base http.RoundTripper
	log  *verbose.Logger
}

// newHTTPClient returns the http.Client to hand the SDK: nil when
// nothing is being logged, so that -v=0 runs on exactly the client the
// SDK builds for itself, and a logging one otherwise. The transport
// mirrors the SDK's own (proxmox.NewSession): same TLS config,
// compression disabled, no proxy from the environment.
func newHTTPClient(tlsConfig *tls.Config, log *verbose.Logger) *http.Client {
	if !log.Enabled(verbose.LevelRequest) {
		return nil
	}
	return &http.Client{
		Transport: &loggingTransport{
			base: &http.Transport{
				TLSClientConfig:    tlsConfig,
				DisableCompression: true,
				Proxy:              nil,
			},
			log: log,
		},
	}
}

// logTask logs a call Proxmox VE performs asynchronously: the flat
// parameters pvectl built at -v=5, and the start and end of the wait at
// -v=4. The returned function takes the outcome.
//
// The wait happens inside the SDK, so this brackets the whole call: the
// reported duration covers the request plus however long the UPID took
// to finish.
func logTask(log *verbose.Logger, what string, params map[string]any) func(error) {
	if log.Enabled(verbose.LevelParams) {
		log.Logf(verbose.LevelParams, "%s: params %s", what, redactParams(params))
	}
	log.Logf(verbose.LevelTask, "task start: %s", what)
	start := time.Now()
	return func(err error) {
		elapsed := time.Since(start).Round(time.Microsecond)
		if err != nil {
			log.Logf(verbose.LevelTask, "task failed: %s after %s: %v", what, elapsed, err)
			return
		}
		log.Logf(verbose.LevelTask, "task done: %s in %s", what, elapsed)
	}
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	wantBody := t.log.Enabled(verbose.LevelBody)

	var reqBody []byte
	if wantBody && req.Body != nil {
		// RoundTrippers may consume and close the request body; they
		// may not otherwise mutate the request, hence the clone.
		body, err := drainBody(req.Body)
		if err != nil {
			return nil, err
		}
		reqBody = body
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start)

	// URL.Redacted masks a password in the userinfo component. pvectl
	// never builds such a URL, but a hand-written server: in the config
	// could.
	if err != nil {
		t.log.Logf(verbose.LevelRequest, "%s %s failed after %s: %v",
			req.Method, req.URL.Redacted(), elapsed.Round(time.Microsecond), err)
		return resp, err
	}

	var respBody []byte
	if wantBody && resp.Body != nil {
		body, readErr := drainBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		respBody = body
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		// A ticket login mints credentials mid-run. Teach them to the
		// logger so every later line that happens to carry one is swept.
		t.log.Secret(ticketSecrets(respBody)...)
	}

	t.log.Logf(verbose.LevelRequest, "%s %s %s in %s",
		req.Method, req.URL.Redacted(), resp.Status, elapsed.Round(time.Microsecond))

	for _, line := range redactHeaders(req.Header) {
		t.log.Logf(verbose.LevelHeaders, "> %s", line)
	}
	for _, line := range redactHeaders(resp.Header) {
		t.log.Logf(verbose.LevelHeaders, "< %s", line)
	}
	if len(reqBody) > 0 {
		t.log.Logf(verbose.LevelBody, "> body: %s", t.preview(redactRequestBody(reqBody)))
	}
	if len(respBody) > 0 {
		t.log.Logf(verbose.LevelBody, "< body: %s", t.preview(redactResponseBody(respBody)))
	}
	return resp, nil
}

// drainBody reads a body to the end and closes it. The caller replaces
// it with a reader over the returned bytes, so the body the SDK sees is
// the one it would have seen without logging.
func drainBody(body io.ReadCloser) ([]byte, error) {
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// preview truncates a body for -v=8. It scrubs before cutting: a secret
// severed by the cut would no longer match the logger's sweep, so the
// sweep has to happen while the string is still whole.
func (t *loggingTransport) preview(body string) string {
	body = t.log.Scrub(body)
	if t.log.Enabled(verbose.LevelFullBody) || len(body) <= bodyPreviewLimit {
		return body
	}
	return body[:bodyPreviewLimit] + "... (truncated; -v=9 for the whole body)"
}

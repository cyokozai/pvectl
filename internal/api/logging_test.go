package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/verbose"
)

// testTokenSecret is the secret half of client_test.go's testToken —
// the part that actually authenticates, and the part that must never
// appear in debug output.
const testTokenSecret = "00000000-0000-0000-0000-000000000000"

// roundTrip drives one request through the logging transport against a
// throwaway server and returns everything that was logged.
func roundTrip(t *testing.T, level int, handler http.HandlerFunc, build func(url string) *http.Request) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	log := verbose.New(level, &buf)
	log.Secret(tokenSecrets(testToken)...)

	client := newHTTPClient(nil, log)
	if client == nil {
		t.Fatalf("newHTTPClient returned nil at -v=%d", level)
	}

	resp, err := client.Do(build(srv.URL))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// The body must still be readable after the transport logged it.
	body, err := readAllClose(resp)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("transport consumed the response body")
	}
	return buf.String()
}

func readAllClose(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func tokenRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		url+"/api2/json/nodes/pve1/qemu", strings.NewReader("vmid=101&name=demo&cipassword=hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "PVEAPIToken="+testToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func okJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Write([]byte(`{"data":"UPID:pve1:00001:qmcreate:101:ci@pam:"}`)) //nolint:errcheck // test server
}

func TestNewHTTPClientOffBelowLevel6(t *testing.T) {
	for _, level := range []int{0, 1, 5} {
		if c := newHTTPClient(nil, verbose.New(level, &bytes.Buffer{})); c != nil {
			t.Errorf("newHTTPClient at -v=%d = %v, want nil so the SDK builds its own", level, c)
		}
	}
}

func TestTransportLevels(t *testing.T) {
	t.Run("v6 summarises the request and nothing more", func(t *testing.T) {
		got := roundTrip(t, verbose.LevelRequest, okJSON, func(u string) *http.Request { return tokenRequest(t, u) })

		if !strings.Contains(got, "POST http://") || !strings.Contains(got, "200 OK in ") {
			t.Errorf("no request summary:\n%s", got)
		}
		if strings.Contains(got, "Authorization") {
			t.Errorf("-v=6 leaked header lines:\n%s", got)
		}
		if strings.Contains(got, "body:") {
			t.Errorf("-v=6 leaked body lines:\n%s", got)
		}
	})

	t.Run("v7 adds masked headers but no bodies", func(t *testing.T) {
		got := roundTrip(t, verbose.LevelHeaders, okJSON, func(u string) *http.Request { return tokenRequest(t, u) })

		if !strings.Contains(got, "[v7] > Authorization: REDACTED") {
			t.Errorf("Authorization header not logged as redacted:\n%s", got)
		}
		if !strings.Contains(got, "[v7] < Content-Type: application/json;charset=UTF-8") {
			t.Errorf("response headers missing:\n%s", got)
		}
		if strings.Contains(got, "body:") {
			t.Errorf("-v=7 leaked body lines:\n%s", got)
		}
	})

	t.Run("v8 adds bodies with credentials masked", func(t *testing.T) {
		got := roundTrip(t, verbose.LevelBody, okJSON, func(u string) *http.Request { return tokenRequest(t, u) })

		if !strings.Contains(got, "[v8] > body: cipassword=REDACTED&name=demo&vmid=101") {
			t.Errorf("request body missing or unmasked:\n%s", got)
		}
		if !strings.Contains(got, `[v8] < body: {"data":"UPID:pve1:00001:qmcreate:101:ci@pam:"}`) {
			t.Errorf("response body missing:\n%s", got)
		}
		if strings.Contains(got, "hunter2") {
			t.Errorf("cloud-init password survived:\n%s", got)
		}
	})
}

func TestTransportTruncatesAtLevel8(t *testing.T) {
	long := strings.Repeat("x", bodyPreviewLimit*2)
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":"` + long + `"}`)) //nolint:errcheck // test server
	}

	at8 := roundTrip(t, verbose.LevelBody, handler, func(u string) *http.Request { return tokenRequest(t, u) })
	if !strings.Contains(at8, "truncated") {
		t.Errorf("-v=8 did not truncate a long body:\n%s", at8[:min(len(at8), 400)])
	}

	at9 := roundTrip(t, verbose.LevelFullBody, handler, func(u string) *http.Request { return tokenRequest(t, u) })
	if strings.Contains(at9, "truncated") {
		t.Error("-v=9 truncated a body")
	}
	if !strings.Contains(at9, long) {
		t.Error("-v=9 did not log the whole body")
	}
}

// TestTransportNeverLogsTheToken is the safety requirement, checked at
// the level that logs the most: an API token configured on the client
// must not reach the output by any route — header, body, or URL.
func TestTransportNeverLogsTheToken(t *testing.T) {
	// The server echoes the credential back in three shapes a careless
	// implementation would print: a header, a JSON field under an
	// innocuous key, and a plain-text error page.
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "PVEAuthCookie="+testTokenSecret)
		w.Header().Set("Csrfpreventiontoken", testTokenSecret)
		w.Write([]byte(`{"data":{"note":"` + r.Header.Get("Authorization") + `"}}`)) //nolint:errcheck // test server
	}

	got := roundTrip(t, verbose.LevelFullBody, handler, func(u string) *http.Request { return tokenRequest(t, u) })

	for _, secret := range []string{testToken, testTokenSecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("the API token reached -v=9 output:\n%s", got)
		}
	}
	if !strings.Contains(got, verbose.Redacted) {
		t.Errorf("nothing was redacted, so the test proved nothing:\n%s", got)
	}
}

// TestTransportLearnsTicketSecrets proves the backstop: a ticket minted
// by the server mid-run is swept out of later lines even though no key
// or header name marks it there.
func TestTransportLearnsTicketSecrets(t *testing.T) {
	const ticket = "PVE:ci@pam:0123456789ABCDEF"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access/ticket") {
			w.Write([]byte(`{"data":{"ticket":"` + ticket + `","CSRFPreventionToken":"csrf-0123456789"}}`)) //nolint:errcheck // test server
			return
		}
		// A later endpoint quotes the ticket back under a harmless key.
		w.Write([]byte(`{"data":{"note":"session ` + ticket + ` is valid"}}`)) //nolint:errcheck // test server
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	client := newHTTPClient(nil, verbose.New(verbose.LevelFullBody, &buf))

	for _, path := range []string{"/api2/json/access/ticket", "/api2/json/cluster/resources"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	if strings.Contains(buf.String(), ticket) {
		t.Errorf("the ticket reached the output:\n%s", buf.String())
	}
}

func TestLogTask(t *testing.T) {
	t.Run("params at v5, start and end at v4", func(t *testing.T) {
		var buf bytes.Buffer
		log := verbose.New(verbose.LevelParams, &buf)
		logTask(log, "POST /nodes/pve1/qemu", map[string]any{"cipassword": "hunter2", "cores": 2})(nil)

		got := buf.String()
		if !strings.Contains(got, "[v5] POST /nodes/pve1/qemu: params {cipassword=REDACTED, cores=2}") {
			t.Errorf("params line missing or unmasked:\n%s", got)
		}
		if !strings.Contains(got, "[v4] task start: POST /nodes/pve1/qemu") {
			t.Errorf("task start missing:\n%s", got)
		}
		if !strings.Contains(got, "[v4] task done: POST /nodes/pve1/qemu in ") {
			t.Errorf("task completion missing:\n%s", got)
		}
	})

	t.Run("a failure is reported at the same level", func(t *testing.T) {
		var buf bytes.Buffer
		logTask(verbose.New(verbose.LevelTask, &buf), "DELETE /nodes/pve1/qemu/101", nil)(context.DeadlineExceeded)

		got := buf.String()
		if !strings.Contains(got, "[v4] task failed: DELETE /nodes/pve1/qemu/101 after ") {
			t.Errorf("failure not reported:\n%s", got)
		}
		if strings.Contains(got, "[v5]") {
			t.Errorf("-v=4 logged parameters:\n%s", got)
		}
	})

	t.Run("a nil logger writes nothing", func(t *testing.T) {
		logTask(nil, "POST /nodes/pve1/qemu", map[string]any{"cores": 2})(nil)
	})
}

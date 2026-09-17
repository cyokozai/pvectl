package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/verbose"
)

// Debug output is written on the assumption that it will be pasted into
// a public issue (.github/ISSUE_TEMPLATE/bug_report.yaml asks for
// terminal output). Everything below therefore masks by key name and
// keeps the key: what was sent stays visible, what would authenticate
// an attacker does not.

// sensitiveHeaders are headers whose value authenticates the caller.
// Compared lowercase.
var sensitiveHeaders = map[string]bool{
	"authorization":       true, // PVEAPIToken=user@realm!id=secret, or PVEAuthCookie=<ticket>
	"cookie":              true, // PVEAuthCookie=<ticket>
	"set-cookie":          true,
	"csrfpreventiontoken": true,
	"proxy-authorization": true,
}

// sensitiveKeys are body and parameter keys whose value is a
// credential. Compared lowercase.
var sensitiveKeys = map[string]bool{
	"password":            true, // login body
	"cipassword":          true, // cloud-init password on a VM config
	"sshkeys":             true, // cloud-init keys; urlencoded blob, never useful in a report
	"ticket":              true, // /access/ticket response
	"csrfpreventiontoken": true, // /access/ticket response
	"pveauthcookie":       true,
	"secret":              true,
	"token":               true,
}

// redactHeaders renders a header set as sorted "Name: value" lines with
// credential values masked.
func redactHeaders(h http.Header) []string {
	lines := make([]string, 0, len(h))
	for name, values := range h {
		if sensitiveHeaders[strings.ToLower(name)] {
			lines = append(lines, name+": "+verbose.Redacted)
			continue
		}
		for _, v := range values {
			lines = append(lines, name+": "+v)
		}
	}
	sort.Strings(lines)
	return lines
}

// redactRequestBody masks a request body. The SDK form-encodes every
// request it sends, so anything that is not obviously JSON is parsed as
// a query string.
func redactRequestBody(body []byte) string {
	s := string(body)
	if looksLikeJSON(s) {
		return redactJSON(s)
	}
	return redactForm(s)
}

// redactResponseBody masks a response body. Proxmox VE answers JSON;
// anything else (an HTML error page from a reverse proxy, a plain-text
// 401) has no key structure to mask, so it is passed through — the
// logger's literal sweep is what covers it.
func redactResponseBody(body []byte) string {
	s := string(body)
	if looksLikeJSON(s) {
		return redactJSON(s)
	}
	return s
}

// redactParams renders a flat Proxmox parameter map deterministically,
// masking credential values. This is what pvectl decided to send,
// logged before it becomes an HTTP body.
func redactParams(params map[string]any) string {
	if len(params) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		value := fmt.Sprintf("%v", params[k])
		if sensitiveKeys[strings.ToLower(k)] {
			value = verbose.Redacted
		}
		parts = append(parts, k+"="+value)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func looksLikeJSON(s string) bool {
	t := strings.TrimLeft(s, " \t\r\n")
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// redactForm masks a urlencoded body and re-encodes it, which also
// sorts the keys — two identical requests produce identical log lines.
// A body that does not parse is returned unchanged rather than mangled.
func redactForm(s string) string {
	values, err := url.ParseQuery(s)
	if err != nil {
		return s
	}
	for key := range values {
		if sensitiveKeys[strings.ToLower(key)] {
			for i := range values[key] {
				values[key][i] = verbose.Redacted
			}
		}
	}
	return values.Encode()
}

// redactJSON masks credential values anywhere in a JSON document and
// re-marshals it. encoding/json sorts object keys, so the rendering is
// deterministic. A document that does not parse is returned unchanged.
func redactJSON(s string) string {
	var doc any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return s
	}
	out, err := json.Marshal(redactValue(doc))
	if err != nil {
		return s
	}
	return string(out)
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		masked := make(map[string]any, len(x))
		for k, val := range x {
			if sensitiveKeys[strings.ToLower(k)] {
				masked[k] = verbose.Redacted
				continue
			}
			masked[k] = redactValue(val)
		}
		return masked
	case []any:
		masked := make([]any, len(x))
		for i, val := range x {
			masked[i] = redactValue(val)
		}
		return masked
	default:
		return v
	}
}

// ticketSecrets pulls the credentials out of an /access/ticket response
// so the logger can sweep them out of every later line. The ticket and
// the CSRF token are minted by the server mid-run: masking them by key
// covers the response that carries them, registering them covers
// everywhere they turn up afterwards.
func ticketSecrets(body []byte) []string {
	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	var found []string
	for _, key := range []string{"ticket", "CSRFPreventionToken"} {
		if s, ok := doc.Data[key].(string); ok && s != "" {
			found = append(found, s)
		}
	}
	return found
}

// tokenSecrets lists the literals an API token string contributes: the
// whole "user@realm!tokenid=secret" and the secret on its own, since
// only the secret half appears in some error messages.
func tokenSecrets(token string) []string {
	if token == "" {
		return nil
	}
	secrets := []string{token}
	if _, secret, ok := strings.Cut(token, "="); ok && secret != "" {
		secrets = append(secrets, secret)
	}
	return secrets
}

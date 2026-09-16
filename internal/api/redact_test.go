package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestRedactHeaders(t *testing.T) {
	h := http.Header{
		"Authorization":       {"PVEAPIToken=ci@pam!pvectl=00000000-0000-0000-0000-000000000000"},
		"Cookie":              {"PVEAuthCookie=PVE:ci@pam:0123456789ABCDEF"},
		"Csrfpreventiontoken": {"0123456789:ABCDEFGH"},
		"Content-Type":        {"application/x-www-form-urlencoded"},
	}

	lines := redactHeaders(h)
	joined := strings.Join(lines, "\n")

	if !slices.IsSorted(lines) {
		t.Errorf("headers are not sorted, so two identical requests log differently:\n%s", joined)
	}
	for _, secret := range []string{"00000000-0000-0000-0000-000000000000", "0123456789ABCDEF", "ABCDEFGH"} {
		if strings.Contains(joined, secret) {
			t.Errorf("credential %q survived redaction:\n%s", secret, joined)
		}
	}
	// The key stays: what was sent must remain visible.
	for _, want := range []string{
		"Authorization: REDACTED",
		"Cookie: REDACTED",
		"Csrfpreventiontoken: REDACTED",
		"Content-Type: application/x-www-form-urlencoded",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
}

func TestRedactRequestBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "cloud-init password is masked, other keys survive",
			body: "cipassword=hunter2&ciuser=admin&cores=2",
			want: "cipassword=REDACTED&ciuser=admin&cores=2",
		},
		{
			name: "login password is masked",
			body: "username=root%40pam&password=hunter2",
			want: "password=REDACTED&username=root%40pam",
		},
		{
			name: "ssh keys are masked",
			body: "sshkeys=ssh-ed25519%20AAAA&name=demo",
			want: "name=demo&sshkeys=REDACTED",
		},
		{
			name: "keys are sorted so identical requests log identically",
			body: "memory=1024&cores=2",
			want: "cores=2&memory=1024",
		},
		{
			name: "an empty body stays empty",
			body: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactRequestBody([]byte(tt.body)); got != tt.want {
				t.Errorf("redactRequestBody(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestRedactResponseBody(t *testing.T) {
	t.Run("a ticket response keeps no credential", func(t *testing.T) {
		body := `{"data":{"ticket":"PVE:ci@pam:0123456789ABCDEF","CSRFPreventionToken":"0123456789:ABCD","username":"ci@pam"}}`
		got := redactResponseBody([]byte(body))

		for _, secret := range []string{"0123456789ABCDEF", "0123456789:ABCD"} {
			if strings.Contains(got, secret) {
				t.Errorf("credential %q survived redaction: %s", secret, got)
			}
		}
		if !strings.Contains(got, `"username":"ci@pam"`) {
			t.Errorf("a non-credential field was lost: %s", got)
		}
	})

	t.Run("credentials nested in arrays are masked too", func(t *testing.T) {
		body := `{"data":[{"cipassword":"hunter2"},{"name":"demo"}]}`
		got := redactResponseBody([]byte(body))
		if strings.Contains(got, "hunter2") {
			t.Errorf("nested credential survived: %s", got)
		}
		if !strings.Contains(got, "demo") {
			t.Errorf("a non-credential field was lost: %s", got)
		}
	})

	t.Run("a non-JSON error page passes through unmangled", func(t *testing.T) {
		body := "401 authentication failure\n"
		if got := redactResponseBody([]byte(body)); got != body {
			t.Errorf("redactResponseBody() = %q, want %q", got, body)
		}
	})
}

func TestRedactParams(t *testing.T) {
	got := redactParams(map[string]any{
		"cores":      2,
		"name":       "demo",
		"cipassword": "hunter2",
	})
	want := "{cipassword=REDACTED, cores=2, name=demo}"
	if got != want {
		t.Errorf("redactParams() = %q, want %q", got, want)
	}
	if got := redactParams(nil); got != "{}" {
		t.Errorf("redactParams(nil) = %q, want {}", got)
	}
}

func TestTicketSecrets(t *testing.T) {
	body := []byte(`{"data":{"ticket":"PVE:ci@pam:0123456789ABCDEF","CSRFPreventionToken":"0123456789:ABCD"}}`)
	got := ticketSecrets(body)
	if len(got) != 2 || got[0] != "PVE:ci@pam:0123456789ABCDEF" || got[1] != "0123456789:ABCD" {
		t.Errorf("ticketSecrets() = %q", got)
	}
	if got := ticketSecrets([]byte("not json")); got != nil {
		t.Errorf("ticketSecrets(non-JSON) = %q, want nil", got)
	}
}

func TestTokenSecrets(t *testing.T) {
	got := tokenSecrets("ci@pam!pvectl=00000000-0000-0000-0000-000000000000")
	want := []string{
		"ci@pam!pvectl=00000000-0000-0000-0000-000000000000",
		"00000000-0000-0000-0000-000000000000",
	}
	if !slices.Equal(got, want) {
		t.Errorf("tokenSecrets() = %q, want %q", got, want)
	}
	if got := tokenSecrets(""); got != nil {
		t.Errorf("tokenSecrets(\"\") = %q, want nil", got)
	}
}

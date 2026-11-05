package redact

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty text", in: "", want: ""},
		{
			name: "a cause without secrets is unchanged",
			in:   "list nodes: Get \"https://10.0.0.5:6443/api/v1/nodes\": dial tcp 10.0.0.5:6443: connect: connection refused",
			want: "list nodes: Get \"https://10.0.0.5:6443/api/v1/nodes\": dial tcp 10.0.0.5:6443: connect: connection refused",
		},
		{
			name: "prose naming a credential keeps its words",
			in:   "password authentication failed for user root; invalid token: expired; missing bearer token",
			want: "password authentication failed for user root; invalid token: expired; missing bearer token",
		},
		{
			name: "URL userinfo is masked and the rest of the URL kept",
			in:   "proxy http://admin:s3cr3t@proxy.lab:3128 refused the tunnel",
			want: "proxy http://[REDACTED]@proxy.lab:3128 refused the tunnel",
		},
		{
			name: "a bearer token is masked",
			in:   "curl -H 'X: 1' Bearer abcdEFGH1234567890._-~+/== failed",
			want: "curl -H 'X: 1' Bearer [REDACTED] failed",
		},
		{
			name: "an Authorization header value is masked through the end of the line",
			in:   "request headers:\nAuthorization: Basic dXNlcjpwYXNz\nAccept: */*",
			want: "request headers:\nAuthorization: [REDACTED]\nAccept: */*",
		},
		{
			name: "a quoted Authorization field keeps its quotes",
			in:   `{"Authorization": "Bearer abcdefghijkl", "Accept": "json"}`,
			want: `{"Authorization": "[REDACTED]", "Accept": "json"}`,
		},
		{
			name: "key=value credentials are masked in queries and options",
			in:   "GET /login?user=root&password=hunter2&next=/ --api-key=abc123 token='x y' becomePassword=\"p w\"",
			want: "GET /login?user=root&password=[REDACTED]&next=/ --api-key=[REDACTED] token='[REDACTED]' becomePassword=\"[REDACTED]\"",
		},
		{
			name: "JSON credential fields are masked and the document stays well formed",
			in:   `{"user":"root","client_secret":"a\"b","sshPrivateKey": 'k', "count": 2}`,
			want: `{"user":"root","client_secret":"[REDACTED]","sshPrivateKey": '[REDACTED]', "count": 2}`,
		},
		{
			name: "a credential line of YAML or a header is masked",
			in:   "users:\n- name: admin\n  token: abc.def.ghi\nX-Api-Key: 0123456789\nkind: Config",
			want: "users:\n- name: admin\n  token: [REDACTED]\nX-Api-Key: [REDACTED]\nkind: Config",
		},
		{
			name: "punctuation that ends a sentence or a chain link is kept",
			in:   "refused token=abc. Retry (password=pw). Get /x?token=t: 401",
			want: "refused token=[REDACTED]. Retry (password=[REDACTED]). Get /x?token=[REDACTED]: 401",
		},
		{
			name: "punctuation inside a secret is masked with it",
			in:   "password=a.b:c(d) next",
			want: "password=[REDACTED] next",
		},
		{
			name: "a JSON Web Token is masked",
			in:   "unauthorized for eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJzd2FsbG93In0.c2lnbmF0dXJlLXZhbHVl at the API",
			want: "unauthorized for [REDACTED] at the API",
		},
		{
			name: "a private key block is masked whole",
			in:   "key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk=\n-----END OPENSSH PRIVATE KEY-----\ndone",
			want: "key:\n[REDACTED PRIVATE KEY]\ndone",
		},
		{
			name: "a private key cut off before its END line is masked to the end",
			in:   "read -----BEGIN RSA PRIVATE KEY-----\nMIIEow",
			want: "read [REDACTED PRIVATE KEY]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.in)
			if got != tc.want {
				t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
			if again := Text(got); again != got {
				t.Errorf("Text is not idempotent on %q: got %q", got, again)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "  ", want: ""},
		{name: "a plain endpoint is unchanged", in: "https://10.0.0.5:6443/api", want: "https://10.0.0.5:6443/api"},
		{name: "userinfo is dropped", in: "https://admin:secret@maas.lab:5240/MAAS/", want: "https://maas.lab:5240/MAAS/"},
		{
			name: "every query value is masked with the keys kept in order",
			in:   "https://api.lab/v1/x?b=2&a=1&flag&token=t",
			want: "https://api.lab/v1/x?b=[REDACTED]&a=[REDACTED]&[REDACTED]&token=[REDACTED]",
		},
		{name: "the fragment is dropped", in: "https://idp.lab/cb#access_token=abc", want: "https://idp.lab/cb"},
		{name: "a host and port is not a URL and is unchanged", in: "10.0.0.5:6443", want: "10.0.0.5:6443"},
		{name: "a non-URL still loses recognized secrets", in: "redis password=abc", want: "redis password=[REDACTED]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := URL(tc.in)
			if got != tc.want {
				t.Errorf("URL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if again := URL(got); again != got {
				t.Errorf("URL is not idempotent on %q: got %q", got, again)
			}
		})
	}
}

// Error keeps the whole wrapped chain readable, so the cause of a failure survives redaction.
func TestError(t *testing.T) {
	if got := Error(nil); got != "" {
		t.Errorf("Error(nil) = %q, want empty", got)
	}
	root := errors.New("401 Unauthorized")
	err := fmt.Errorf("list nodes: Get https://ops:pw@10.0.0.5:6443/api?token=abc: %w", root)
	got := Error(err)
	want := "list nodes: Get https://[REDACTED]@10.0.0.5:6443/api?token=[REDACTED]: 401 Unauthorized"
	if got != want {
		t.Errorf("Error(%v) = %q, want %q", err, got, want)
	}
	if strings.Contains(got, "pw@") || strings.Contains(got, "abc") {
		t.Errorf("Error(%v) = %q still holds a secret", err, got)
	}
}

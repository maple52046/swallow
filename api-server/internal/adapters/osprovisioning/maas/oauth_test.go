package maas

import (
	"strings"
	"testing"
)

func TestParseAPIKey_Valid(t *testing.T) {
	key, err := ParseAPIKey("  consumer:token:secret  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.ConsumerKey != "consumer" || key.TokenKey != "token" || key.TokenSecret != "secret" {
		t.Fatalf("unexpected split: %+v", key)
	}
}

func TestParseAPIKey_Invalid(t *testing.T) {
	cases := map[string]string{
		"too few parts":  "consumer:token",
		"too many parts": "consumer:token:secret:extra",
		"empty part":     "consumer::secret",
		"empty":          "",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAPIKey(raw); err == nil {
				t.Fatalf("expected an error for %q", raw)
			}
		})
	}
}

// The key must never appear in an error, because config errors reach logs.
func TestParseAPIKey_ErrorDoesNotLeakKey(t *testing.T) {
	parts := []string{"Xk8fQ2", "Vt5nR9", "Zs3wL7", "Bq1yM4"}

	_, err := ParseAPIKey(strings.Join(parts, ":"))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, part := range parts {
		if strings.Contains(err.Error(), part) {
			t.Fatalf("error message leaked %q: %s", part, err)
		}
	}
}

func TestAuthorizationHeader_PlaintextSignature(t *testing.T) {
	key := APIKey{ConsumerKey: "ck", TokenKey: "tk", TokenSecret: "ts"}

	got := key.authorizationHeader("nonce123", 1700000000)

	want := `OAuth oauth_version="1.0", oauth_signature_method="PLAINTEXT", ` +
		`oauth_consumer_key="ck", oauth_token="tk", oauth_signature="&ts", ` +
		`oauth_nonce="nonce123", oauth_timestamp="1700000000"`
	if got != want {
		t.Fatalf("unexpected header:\n got: %s\nwant: %s", got, want)
	}
}

func TestNewNonce_Varies(t *testing.T) {
	first, err := newNonce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := newNonce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first == second {
		t.Fatal("expected consecutive nonces to differ")
	}
}

func TestNormalizeAPIRoot(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"maas path", "http://maas:5240/MAAS", "http://maas:5240/MAAS/api/2.0"},
		{"trailing slash", "http://maas:5240/MAAS/", "http://maas:5240/MAAS/api/2.0"},
		{"already api root", "http://maas:5240/MAAS/api/2.0", "http://maas:5240/MAAS/api/2.0"},
		{"bare host gets default prefix", "http://maas:5240", "http://maas:5240/MAAS/api/2.0"},
		{"https custom prefix", "https://maas.example.com/maas-prod", "https://maas.example.com/maas-prod/api/2.0"},
		{"surrounding spaces", "  http://maas:5240/MAAS  ", "http://maas:5240/MAAS/api/2.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAPIRoot(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeAPIRoot_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"no scheme":    "maas:5240/MAAS",
		"wrong scheme": "ftp://maas:5240/MAAS",
		"missing host": "http:///MAAS",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeAPIRoot(raw); err == nil {
				t.Fatalf("expected an error for %q", raw)
			}
		})
	}
}

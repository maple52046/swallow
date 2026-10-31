package domain

import (
	"errors"
	"testing"
)

func TestNormalizeHTTPProxy(t *testing.T) {
	tests := []struct {
		name  string
		input HTTPProxyConfiguration
		want  HTTPProxyConfiguration
	}{
		{"none", HTTPProxyConfiguration{Mode: "none"}, HTTPProxyConfiguration{Mode: HTTPProxyModeNone}},
		{"builtin with surrounding whitespace", HTTPProxyConfiguration{Mode: " Builtin ", URL: "  "},
			HTTPProxyConfiguration{Mode: HTTPProxyModeBuiltin}},
		{"external keeps host and port", HTTPProxyConfiguration{Mode: "external", URL: "http://10.170.168.20:18080"},
			HTTPProxyConfiguration{Mode: HTTPProxyModeExternal, URL: "http://10.170.168.20:18080"}},
		{"trailing slash removed", HTTPProxyConfiguration{Mode: "peer", URL: " http://proxy.example.com:3128/ "},
			HTTPProxyConfiguration{Mode: HTTPProxyModePeer, URL: "http://proxy.example.com:3128"}},
		{"https and IPv6 host", HTTPProxyConfiguration{Mode: "external", URL: "HTTPS://[fd00::1]:3128"},
			HTTPProxyConfiguration{Mode: HTTPProxyModeExternal, URL: "https://[fd00::1]:3128"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeHTTPProxy(tc.input)
			if err != nil {
				t.Fatalf("NormalizeHTTPProxy(%+v) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeHTTPProxy(%+v) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

// A URL that does not apply, is missing, or is anything but scheme://host[:port] is refused before
// the provisioner sees it; credentials are refused because the setting is readable back.
func TestNormalizeHTTPProxyRefusals(t *testing.T) {
	for name, input := range map[string]HTTPProxyConfiguration{
		"unknown mode":         {Mode: "squid"},
		"empty mode":           {Mode: ""},
		"URL with none":        {Mode: "none", URL: "http://proxy:3128"},
		"URL with builtin":     {Mode: "builtin", URL: "http://proxy:3128"},
		"external without URL": {Mode: "external"},
		"peer without URL":     {Mode: "peer", URL: "  "},
		"no scheme":            {Mode: "external", URL: "proxy.example.com:3128"},
		"ftp scheme":           {Mode: "external", URL: "ftp://proxy:3128"},
		"no host":              {Mode: "external", URL: "http://:3128"},
		"credentials":          {Mode: "external", URL: "http://user:secret@proxy:3128"},
		"user only":            {Mode: "peer", URL: "http://user@proxy:3128"},
		"port out of range":    {Mode: "external", URL: "http://proxy:70000"},
		"path":                 {Mode: "external", URL: "http://proxy:3128/squid"},
		"query":                {Mode: "external", URL: "http://proxy:3128/?x=1"},
		"fragment":             {Mode: "external", URL: "http://proxy:3128/#a"},
		"inner space":          {Mode: "external", URL: "http://proxy :3128"},
	} {
		if got, err := NormalizeHTTPProxy(input); !errors.Is(err, ErrInvalidHTTPProxy) {
			t.Errorf("%s: NormalizeHTTPProxy(%+v) = %+v, %v; want ErrInvalidHTTPProxy", name, input, got, err)
		}
	}
}

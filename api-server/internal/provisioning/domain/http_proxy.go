package domain

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// This file holds the Provisioner HTTP Proxy model (decision 056): the proxy a provisioner hands to
// the machines it deploys, as a closed mode set plus a URL. It is pure model code: provider adapters
// under infra/ translate it to their own settings, which never appear here.

// HTTPProxyMode is the closed value set of a Provisioner HTTP Proxy (glossary Provisioner HTTP
// Proxy, decision 056): which proxy a provisioner hands to the machines it deploys. The values are
// domain language that flows unchanged through the API; adapters translate them to and from their
// provider's settings and must map every provider combination onto one of them.
type HTTPProxyMode string

const (
	// HTTPProxyModeNone means deployed machines reach package repositories directly.
	HTTPProxyModeNone HTTPProxyMode = "none"
	// HTTPProxyModeBuiltin means deployed machines use the provisioner's own proxy, which goes out
	// directly.
	HTTPProxyModeBuiltin HTTPProxyMode = "builtin"
	// HTTPProxyModeExternal means deployed machines, and the provisioner itself, use the configured
	// URL directly.
	HTTPProxyModeExternal HTTPProxyMode = "external"
	// HTTPProxyModePeer means deployed machines use the provisioner's own proxy, which forwards to
	// the configured URL as its upstream peer.
	HTTPProxyModePeer HTTPProxyMode = "peer"
)

// NeedsURL reports whether the mode routes traffic through an operator-given proxy, which is the
// only case where HTTPProxyConfiguration.URL applies.
func (m HTTPProxyMode) NeedsURL() bool {
	return m == HTTPProxyModeExternal || m == HTTPProxyModePeer
}

// HTTPProxyConfiguration is one provisioner's Provisioner HTTP Proxy. The provisioner owns it;
// swallow reads it live and writes it through, never stores it, and a write replaces Mode and URL
// together.
//
// URL is "" unless Mode.NeedsURL, so a configuration read from a provider can be written back
// unchanged. It never carries credentials: NormalizeHTTPProxy refuses them, because the setting is
// readable back through the API.
type HTTPProxyConfiguration struct {
	Mode HTTPProxyMode
	URL  string
}

// ErrInvalidHTTPProxy marks a Provisioner HTTP Proxy that swallow refuses before calling the
// provisioner: an unknown mode, or a URL that is missing, malformed, or not applicable to the mode.
// Errors wrapping it carry the operator-facing reason.
var ErrInvalidHTTPProxy = errors.New("invalid provisioner HTTP proxy")

// NormalizeHTTPProxy validates an operator's configuration and returns its canonical form: the mode
// lower-cased and the URL reduced to scheme://host[:port], without surrounding whitespace or a
// trailing "/". A refusal wraps ErrInvalidHTTPProxy with the reason.
//
// The URL must be an absolute http or https address with a host. A user or password, a query, a
// fragment, or a path other than "/" is refused: a proxy for package traffic is addressed by host
// and port only, and credentials would be returned to every later reader.
func NormalizeHTTPProxy(config HTTPProxyConfiguration) (HTTPProxyConfiguration, error) {
	mode := HTTPProxyMode(strings.ToLower(strings.TrimSpace(string(config.Mode))))
	raw := strings.TrimSpace(config.URL)
	switch mode {
	case HTTPProxyModeNone, HTTPProxyModeBuiltin:
		if raw != "" {
			return HTTPProxyConfiguration{}, fmt.Errorf("%w: httpProxy does not apply to mode %q", ErrInvalidHTTPProxy, mode)
		}
		return HTTPProxyConfiguration{Mode: mode}, nil
	case HTTPProxyModeExternal, HTTPProxyModePeer:
		if raw == "" {
			return HTTPProxyConfiguration{}, fmt.Errorf("%w: httpProxy is required for mode %q", ErrInvalidHTTPProxy, mode)
		}
		normalized, err := normalizeProxyURL(raw)
		if err != nil {
			return HTTPProxyConfiguration{}, err
		}
		return HTTPProxyConfiguration{Mode: mode, URL: normalized}, nil
	default:
		return HTTPProxyConfiguration{}, fmt.Errorf("%w: mode %q is not one of none, builtin, external, peer",
			ErrInvalidHTTPProxy, config.Mode)
	}
}

// normalizeProxyURL applies the URL rules of NormalizeHTTPProxy to a trimmed, non-empty value.
func normalizeProxyURL(raw string) (string, error) {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: httpProxy %s", ErrInvalidHTTPProxy, reason)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return "", invalid("must not contain spaces")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", invalid("is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", invalid("must start with http:// or https://")
	}
	if u.User != nil {
		return "", invalid("must not carry a user or password")
	}
	if u.Hostname() == "" {
		return "", invalid("must name a host")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", invalid("must have a port between 1 and 65535")
		}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("must not have a query or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return "", invalid("must not have a path")
	}
	return u.Scheme + "://" + u.Host, nil
}

// HTTPProxyReader reads a provisioner's Provisioner HTTP Proxy live. An adapter that sets
// ProviderCapabilities.HTTPProxy must implement it.
//
// Implementations map every combination of their provider's settings onto one mode, report URL
// only for modes that need it, and return the usual *ProviderError kinds when the provider cannot
// be reached or refuses the credential.
type HTTPProxyReader interface {
	HTTPProxy(ctx context.Context) (HTTPProxyConfiguration, error)
}

// HTTPProxyWriter writes a Provisioner HTTP Proxy through to the provisioner. An adapter that sets
// ProviderCapabilities.HTTPProxy must implement it.
//
// config has already been normalized. Implementations replace every provider setting the mode
// covers, so a following read reports config. A provider that applies settings one at a time may
// fail part-way; it returns the error and leaves the provider in a state the next read reports,
// and repeating the write converges. A refusal by the provider is a *ProviderError{Kind:
// ProviderErrorRejected} carrying its explanation, a credential that may not change provider
// settings is ProviderErrorAuth, and transport failures are ProviderErrorUnavailable. A write only
// affects deployments the provider starts afterwards.
type HTTPProxyWriter interface {
	SetHTTPProxy(ctx context.Context, config HTTPProxyConfiguration) error
}

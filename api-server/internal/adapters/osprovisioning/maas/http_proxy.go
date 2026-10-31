package maas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// This file implements the Provisioner HTTP Proxy capability against MAAS (decision 056). MAAS holds
// the proxy as three region settings on the /maas/ handler, read with op=get_config and written with
// op=set_config one setting per call. The setting names stay here.

// maasHandlerPath is the MAAS region handler that serves get_config and set_config.
const maasHandlerPath = "/maas/"

// The MAAS region settings behind the Provisioner HTTP Proxy.
const (
	settingEnableHTTPProxy = "enable_http_proxy"
	settingHTTPProxy       = "http_proxy"
	settingUsePeerProxy    = "use_peer_proxy"
)

// HTTPProxy implements provisioningdomain.HTTPProxyReader with three get_config reads. MAAS answers
// each with the JSON value of the setting (a boolean, or a string that is null when never set); a
// null is read as false or "".
func (p *Provider) HTTPProxy(ctx context.Context) (provisioningdomain.HTTPProxyConfiguration, error) {
	var enabled, peer *bool
	var proxyURL *string
	if err := p.readSetting(ctx, settingEnableHTTPProxy, &enabled); err != nil {
		return provisioningdomain.HTTPProxyConfiguration{}, err
	}
	if err := p.readSetting(ctx, settingHTTPProxy, &proxyURL); err != nil {
		return provisioningdomain.HTTPProxyConfiguration{}, err
	}
	if err := p.readSetting(ctx, settingUsePeerProxy, &peer); err != nil {
		return provisioningdomain.HTTPProxyConfiguration{}, err
	}
	return httpProxyFromSettings(enabled != nil && *enabled, peer != nil && *peer, proxyURL), nil
}

// httpProxyFromSettings derives the mode the way MAAS decides which proxy a deployed machine's APT
// configuration names (its get_apt_proxy): no proxy when disabled; the built-in proxy when no URL is
// set, whatever use_peer_proxy says; otherwise the URL directly, or through the built-in proxy when
// use_peer_proxy is set. Every combination therefore lands on the mode that describes its effect.
func httpProxyFromSettings(enabled, peer bool, proxyURL *string) provisioningdomain.HTTPProxyConfiguration {
	value := ""
	if proxyURL != nil {
		value = strings.TrimSpace(*proxyURL)
	}
	switch {
	case !enabled:
		return provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeNone}
	case value == "":
		return provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin}
	case peer:
		return provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModePeer, URL: value}
	default:
		return provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: value}
	}
}

// SetHTTPProxy implements provisioningdomain.HTTPProxyWriter. Every mode writes all three settings,
// as the MAAS UI does, so stale values cannot change the effect of the new mode: enable_http_proxy
// first, then http_proxy (an explicit "" clears it), then use_peer_proxy. set_config is admin-only
// and MAAS applies each call on its own, so a failure part-way leaves the earlier settings written.
func (p *Provider) SetHTTPProxy(ctx context.Context, config provisioningdomain.HTTPProxyConfiguration) error {
	enabled, peer := "true", "false"
	switch config.Mode {
	case provisioningdomain.HTTPProxyModeNone:
		enabled = "false"
	case provisioningdomain.HTTPProxyModeBuiltin, provisioningdomain.HTTPProxyModeExternal:
		// Enabled without a peer: the defaults above. The URL tells the two apart.
	case provisioningdomain.HTTPProxyModePeer:
		peer = "true"
	default:
		return fmt.Errorf("%w: mode %q cannot be written to MAAS", provisioningdomain.ErrInvalidHTTPProxy, config.Mode)
	}
	proxyURL := ""
	if config.Mode.NeedsURL() {
		proxyURL = config.URL
	}
	for _, setting := range []struct{ name, value string }{
		{settingEnableHTTPProxy, enabled},
		{settingHTTPProxy, proxyURL},
		{settingUsePeerProxy, peer},
	} {
		fields := map[string]string{"name": setting.name, "value": setting.value}
		if err := p.client.postOperationExact(ctx, maasHandlerPath, "set_config", fields, nil); err != nil {
			return translateSettingWriteError(err)
		}
	}
	return nil
}

// readSetting decodes the JSON value MAAS returns for one region setting into out. get_config is
// open to any MAAS user, so a refused read means the key itself was rejected and takes the generic
// translation, not the administrator message of a write.
func (p *Provider) readSetting(ctx context.Context, name string, out any) error {
	query := url.Values{}
	query.Set("op", "get_config")
	query.Set("name", name)
	if err := p.client.get(ctx, maasHandlerPath, query, out); err != nil {
		return translateError(err, "")
	}
	return nil
}

// translateSettingWriteError keeps a refused set_config explicit: MAAS answers 401/403 when the
// integration's account is not an administrator, which the operator must fix in the Integration, not
// a rejected value.
func translateSettingWriteError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorAuth,
			Detail: "The provisioner credential may not change MAAS settings; its MAAS account must be an administrator.",
			Err:    err,
		}
	}
	return translateError(err, "")
}

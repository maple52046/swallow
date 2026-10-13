package maas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// Compile-time proof that the adapter offers the Provisioner HTTP Proxy capability.
var (
	_ provisioningdomain.HTTPProxyReader = (*Provider)(nil)
	_ provisioningdomain.HTTPProxyWriter = (*Provider)(nil)
)

// settingsFake serves MAAS region settings through /maas/ get_config and set_config. Values are
// held as raw JSON, the way get_config returns them; set_config records each write in order and
// stores booleans and strings the way the MAAS config form cleans them.
type settingsFake struct {
	*fakeMAAS
	mu        sync.Mutex
	values    map[string]string
	writes    [][2]string
	setStatus int
	setBody   string
}

func newSettingsFake(t *testing.T, values map[string]string) *settingsFake {
	t.Helper()
	f := &settingsFake{fakeMAAS: newFakeMAAS(t), values: values, setStatus: http.StatusOK}
	f.mux.HandleFunc("GET "+apiPrefix+"/maas/{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("op") != "get_config" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		value, ok := f.values[r.URL.Query().Get("name")]
		f.mu.Unlock()
		if !ok {
			value = "null"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(value))
	})
	f.mux.HandleFunc("POST "+apiPrefix+"/maas/{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("op") != "set_config" || r.MultipartForm == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		name, value := r.MultipartForm.Value["name"], r.MultipartForm.Value["value"]
		if len(name) != 1 || len(value) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("No provided value!"))
			return
		}
		if f.setStatus != http.StatusOK {
			w.WriteHeader(f.setStatus)
			_, _ = w.Write([]byte(f.setBody))
			return
		}
		f.mu.Lock()
		f.writes = append(f.writes, [2]string{name[0], value[0]})
		stored := value[0]
		if stored != "true" && stored != "false" {
			encoded, _ := json.Marshal(stored) // marshalling a string cannot fail
			stored = string(encoded)
		}
		f.values[name[0]] = stored
		f.mu.Unlock()
		_, _ = w.Write([]byte("OK"))
	})
	return f
}

// Every combination of the three MAAS settings maps onto the mode that describes its effect, the
// way MAAS picks a deployed machine's APT proxy.
func TestHTTPProxy_DerivesModeFromSettings(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   provisioningdomain.HTTPProxyConfiguration
	}{
		{"disabled keeps no URL", map[string]string{"enable_http_proxy": "false", "http_proxy": `"http://old:3128/"`, "use_peer_proxy": "true"},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeNone}},
		{"enabled without URL is built-in", map[string]string{"enable_http_proxy": "true", "http_proxy": "null", "use_peer_proxy": "false"},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin}},
		{"peer without URL is built-in", map[string]string{"enable_http_proxy": "true", "http_proxy": `""`, "use_peer_proxy": "true"},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin}},
		{"URL is external", map[string]string{"enable_http_proxy": "true", "http_proxy": `"http://10.170.168.20:18080"`, "use_peer_proxy": "false"},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: "http://10.170.168.20:18080"}},
		{"URL with peer is peer", map[string]string{"enable_http_proxy": "true", "http_proxy": `"http://squid:3128/"`, "use_peer_proxy": "true"},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModePeer, URL: "http://squid:3128/"}},
		{"unset settings read as disabled", map[string]string{},
			provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeNone}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newSettingsFake(t, tc.values)
			got, err := newTestProvider(t, fake.fakeMAAS).HTTPProxy(context.Background())
			if err != nil {
				t.Fatalf("HTTPProxy error = %v", err)
			}
			if got != tc.want {
				t.Errorf("HTTPProxy() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A write sets all three settings in order, clears http_proxy with an explicit empty value, and is
// read back as written.
func TestSetHTTPProxy_WritesAllSettings(t *testing.T) {
	tests := []struct {
		config provisioningdomain.HTTPProxyConfiguration
		want   [][2]string
	}{
		{provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: "http://10.170.168.20:18080"},
			[][2]string{{"enable_http_proxy", "true"}, {"http_proxy", "http://10.170.168.20:18080"}, {"use_peer_proxy", "false"}}},
		{provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModePeer, URL: "http://squid:3128"},
			[][2]string{{"enable_http_proxy", "true"}, {"http_proxy", "http://squid:3128"}, {"use_peer_proxy", "true"}}},
		{provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin},
			[][2]string{{"enable_http_proxy", "true"}, {"http_proxy", ""}, {"use_peer_proxy", "false"}}},
		{provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeNone},
			[][2]string{{"enable_http_proxy", "false"}, {"http_proxy", ""}, {"use_peer_proxy", "false"}}},
	}
	for _, tc := range tests {
		t.Run(string(tc.config.Mode), func(t *testing.T) {
			fake := newSettingsFake(t, map[string]string{
				"enable_http_proxy": "true", "http_proxy": `"http://stale:3128/"`, "use_peer_proxy": "true",
			})
			provider := newTestProvider(t, fake.fakeMAAS)
			if err := provider.SetHTTPProxy(context.Background(), tc.config); err != nil {
				t.Fatalf("SetHTTPProxy(%+v) error = %v", tc.config, err)
			}
			if len(fake.writes) != len(tc.want) {
				t.Fatalf("SetHTTPProxy(%+v) wrote %v, want %v", tc.config, fake.writes, tc.want)
			}
			for i := range tc.want {
				if fake.writes[i] != tc.want[i] {
					t.Errorf("SetHTTPProxy(%+v) write %d = %v, want %v", tc.config, i, fake.writes[i], tc.want[i])
				}
			}
			got, err := provider.HTTPProxy(context.Background())
			if err != nil || got != tc.config {
				t.Errorf("HTTPProxy() after write = %+v, %v; want %+v", got, err, tc.config)
			}
		})
	}
}

// set_config is admin-only in MAAS: a non-admin account is an Integration setup problem (auth),
// while a value MAAS refuses keeps its explanation as a rejection.
func TestSetHTTPProxy_Refusals(t *testing.T) {
	config := provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: "http://proxy:3128"}

	fake := newSettingsFake(t, map[string]string{})
	fake.setStatus = http.StatusForbidden
	err := newTestProvider(t, fake.fakeMAAS).SetHTTPProxy(context.Background(), config)
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorAuth {
		t.Errorf("SetHTTPProxy as non-admin error = %v, want ProviderErrorAuth", err)
	}

	fake = newSettingsFake(t, map[string]string{})
	fake.setStatus, fake.setBody = http.StatusBadRequest, `{"http_proxy": ["Enter a valid URL."]}`
	err = newTestProvider(t, fake.fakeMAAS).SetHTTPProxy(context.Background(), config)
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("SetHTTPProxy refused by MAAS error = %v, want ProviderErrorRejected", err)
	}

	err = newTestProvider(t, fake.fakeMAAS).SetHTTPProxy(context.Background(), provisioningdomain.HTTPProxyConfiguration{Mode: "squid"})
	if !errors.Is(err, provisioningdomain.ErrInvalidHTTPProxy) {
		t.Errorf("SetHTTPProxy with an unknown mode error = %v, want ErrInvalidHTTPProxy", err)
	}
}

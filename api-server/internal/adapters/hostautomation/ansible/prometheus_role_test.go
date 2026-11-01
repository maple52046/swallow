package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scrape configuration is rendered by a rename, which a single-file bind mount does not follow:
// the container keeps the old inode, so a reload re-reads the old configuration. Both container
// methods mount the configuration directory instead.
func TestPrometheusContainersMountTheConfigDirectory(t *testing.T) {
	for _, template := range []string{"compose.yaml.j2", "prometheus.container.j2"} {
		raw, err := os.ReadFile(shippedFile(t, filepath.Join("roles", "prometheus", "templates", template)))
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		if !strings.Contains(content, "{{ prometheus_config_dir }}:/etc/prometheus:ro") {
			t.Errorf("%s does not mount the configuration directory", template)
		}
		if strings.Contains(content, "prometheus.yml:/etc/prometheus/prometheus.yml") {
			t.Errorf("%s mounts the configuration file, which a rename leaves stale", template)
		}
	}
}

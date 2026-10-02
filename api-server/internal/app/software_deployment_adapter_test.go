package app

import (
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// Workflow intents and Step names use the product's written name, keeping meaningful
// capitalization such as the CE in Docker CE, and fall back to the slug for an unknown kind.
func TestSoftwareLabelUsesCatalogName(t *testing.T) {
	tests := map[softwaredomain.Kind]string{
		softwaredomain.KindDockerCE: "Docker CE",
		softwaredomain.KindPodman:   "Podman",
		softwaredomain.KindNFS:      "NFS",
		"future-kind":               "future-kind",
	}
	for kind, want := range tests {
		if got := softwareLabel(kind); got != want {
			t.Errorf("softwareLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}

package domain

import (
	"errors"
	"testing"
)

func TestParseImageReference(t *testing.T) {
	tests := []struct {
		reference     string
		wantName      string
		wantTag       string
		wantCanonical string
	}{
		{"nginx", "nginx", "latest", "nginx:latest"},
		{"nginx:1.27", "nginx", "1.27", "nginx:1.27"},
		{"registry.local:5000/team/app", "registry.local:5000/team/app", "latest", "registry.local:5000/team/app:latest"},
		{"registry.local:5000/team/app:v2", "registry.local:5000/team/app", "v2", "registry.local:5000/team/app:v2"},
		{"nginx@sha256:abcd", "nginx", "sha256:abcd", "nginx@sha256:abcd"},
		{"  nginx:1.27  ", "nginx", "1.27", "nginx:1.27"},
	}
	for _, tc := range tests {
		t.Run(tc.reference, func(t *testing.T) {
			name, tag, canonical, err := ParseImageReference(tc.reference)
			if err != nil {
				t.Fatalf("ParseImageReference(%q) error = %v", tc.reference, err)
			}
			if name != tc.wantName || tag != tc.wantTag || canonical != tc.wantCanonical {
				t.Errorf("ParseImageReference(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.reference, name, tag, canonical, tc.wantName, tc.wantTag, tc.wantCanonical)
			}
		})
	}
	for _, bad := range []string{"", "nginx latest", "nginx:", "@sha256:abcd", "nginx@"} {
		if _, _, _, err := ParseImageReference(bad); !errors.Is(err, ErrInvalidDockerRequest) {
			t.Errorf("ParseImageReference(%q) error = %v, want ErrInvalidDockerRequest", bad, err)
		}
	}
}

func TestDockerContainerSpecValidate(t *testing.T) {
	valid := DockerContainerSpec{
		Name: "web", Image: "nginx:1.27", Env: []string{"A=1", "EMPTY="},
		Ports:         []DockerPortBinding{{ContainerPort: 80, HostPort: 8080, Protocol: "tcp"}, {ContainerPort: 53, Protocol: "udp"}},
		Volumes:       []DockerVolumeBinding{{Source: "web-data", Target: "/data"}, {Source: "/srv/www", Target: "/www", ReadOnly: true}},
		RestartPolicy: "unless-stopped",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid spec) error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(spec *DockerContainerSpec)
	}{
		{"missing image", func(spec *DockerContainerSpec) { spec.Image = " " }},
		{"bad name", func(spec *DockerContainerSpec) { spec.Name = "-web" }},
		{"env without key", func(spec *DockerContainerSpec) { spec.Env = []string{"=1"} }},
		{"env without equals", func(spec *DockerContainerSpec) { spec.Env = []string{"A"} }},
		{"container port zero", func(spec *DockerContainerSpec) { spec.Ports = []DockerPortBinding{{ContainerPort: 0}} }},
		{"host port too large", func(spec *DockerContainerSpec) {
			spec.Ports = []DockerPortBinding{{ContainerPort: 80, HostPort: 70000}}
		}},
		{"bad protocol", func(spec *DockerContainerSpec) {
			spec.Ports = []DockerPortBinding{{ContainerPort: 80, Protocol: "icmp"}}
		}},
		{"bad host ip", func(spec *DockerContainerSpec) { spec.Ports = []DockerPortBinding{{ContainerPort: 80, HostIP: "nope"}} }},
		{"relative target", func(spec *DockerContainerSpec) {
			spec.Volumes = []DockerVolumeBinding{{Source: "data", Target: "data"}}
		}},
		{"bad volume name", func(spec *DockerContainerSpec) {
			spec.Volumes = []DockerVolumeBinding{{Source: "my data", Target: "/data"}}
		}},
		{"colon in path", func(spec *DockerContainerSpec) {
			spec.Volumes = []DockerVolumeBinding{{Source: "/a:b", Target: "/data"}}
		}},
		{"unknown restart policy", func(spec *DockerContainerSpec) { spec.RestartPolicy = "sometimes" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := valid
			tc.mutate(&spec)
			if err := spec.Validate(); !errors.Is(err, ErrInvalidDockerRequest) {
				t.Errorf("Validate() error = %v, want ErrInvalidDockerRequest", err)
			}
		})
	}
}

func TestDockerNetworkSpecValidate(t *testing.T) {
	tests := []struct {
		name  string
		spec  DockerNetworkSpec
		valid bool
	}{
		{"name only", DockerNetworkSpec{Name: "app-net"}, true},
		{"subnet and gateway", DockerNetworkSpec{Name: "app-net", Subnet: "172.20.0.0/16", Gateway: "172.20.0.1"}, true},
		{"missing name", DockerNetworkSpec{}, false},
		{"predefined name", DockerNetworkSpec{Name: "host"}, false},
		{"gateway without subnet", DockerNetworkSpec{Name: "app-net", Gateway: "172.20.0.1"}, false},
		{"bad subnet", DockerNetworkSpec{Name: "app-net", Subnet: "172.20.0.0"}, false},
		{"bad gateway", DockerNetworkSpec{Name: "app-net", Subnet: "172.20.0.0/16", Gateway: "gw"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if tc.valid && err != nil {
				t.Errorf("Validate(%+v) error = %v, want nil", tc.spec, err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidDockerRequest) {
				t.Errorf("Validate(%+v) error = %v, want ErrInvalidDockerRequest", tc.spec, err)
			}
		})
	}
}

// Only an explicit true enables the API; a legacy spec without the key must read as disabled.
func TestDockerAPIEnabled(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want bool
	}{
		{"explicit true", map[string]any{"enableApi": true}, true},
		{"explicit false", map[string]any{"enableApi": false}, false},
		{"legacy nil spec", nil, false},
		{"legacy spec without key", map[string]any{"version": "27"}, false},
		{"string true is not a boolean", map[string]any{"enableApi": "true"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DockerAPIEnabled(tc.spec); got != tc.want {
				t.Errorf("DockerAPIEnabled(%v) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}

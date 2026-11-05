package ansible

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestK0sPlaybookSupportsFlexibleTopologiesAndLegacyRetries(t *testing.T) {
	cases := []struct {
		path     string
		required []string
	}{
		{
			path: "deploy-kubernetes.yml",
			required: []string{
				"swallow_k0s_workload_controller_ids | default([])",
				"k0s_runs_workloads",
				"role: k0s_host_preflight",
			},
		},
		{
			path: filepath.Join("roles", "k0s_host_preflight", "tasks", "main.yml"),
			required: []string{
				"/sys/fs/cgroup/cgroup.controllers",
				"xt_REDIRECT",
				"iptable_nat",
				"/usr/local/bin/k0s",
				"sysinfo",
				"k0s_sysinfo.stdout_lines",
				"swallow_k0s_ephemeral_root | default(false) | bool",
				"ephemeral-snapshotter.toml",
				"snapshotter = \"native\"",
				"use_local_image_pull = true",
				// An HA VIP whose prefix differs from the host subnet makes etcd reject the VIP holder.
				"swallow_k0s_api_vip_prefix | int != k0s_pf_host_ipv4.prefix | int",
			},
		},
		{
			path: filepath.Join("roles", "k0s_config", "templates", "k0s.yaml.j2"),
			required: []string{
				"swallow_k0s_api_address | default(hostvars[swallow_k0s_initial_controller_id].ansible_host, true)",
				// A controller never auto-detects its API address: join tokens embed it.
				"address: {{ ansible_host }}",
				"swallow_k0s_high_availability | default(true)",
				"{% if k0s_ha %}",
			},
		},
		{
			path:     filepath.Join("roles", "k0s_controller_bootstrap", "tasks", "main.yml"),
			required: []string{"--enable-worker --no-taints", "when: k0s_runs_workloads | bool"},
		},
		{
			path: filepath.Join("roles", "k0s_cluster_credential", "tasks", "main.yml"),
			required: []string{
				"swallow_k0s_api_address | default(hostvars[swallow_k0s_initial_controller_id].ansible_host, true)",
				// The cluster CA comes from the reader token secret and is returned with the token.
				"data['ca.crt']",
				"'caCertificate': k0s_cluster_ca",
				// The operator token is minted beside the reader token and returned with it.
				"get secret swallow-operator-token",
				"'operatorToken': k0s_operator_token",
				// A redeploy narrows the reader token in place; a cluster without the old binding is fine.
				"{{ k0s_cluster_credential_legacy_reader_binding }} --ignore-not-found",
			},
		},
		{
			path:     filepath.Join("roles", "k0s_verify", "tasks", "main.yml"),
			required: []string{"swallow_k0s_workload_ids | default(swallow_k0s_worker_ids)"},
		},
		{
			path: filepath.Join("roles", "k0s_worker_join", "tasks", "main.yml"),
			required: []string{
				"Ensure /etc/k0s exists",
				"path: /etc/k0s",
				"state: directory",
				"dest: /etc/k0s/join-token",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile(shippedFile(t, tc.path))
			if err != nil {
				t.Fatalf("read playbook artifact: %v", err)
			}
			content := string(raw)
			for _, required := range tc.required {
				if !strings.Contains(content, required) {
					t.Errorf("%s missing %q", tc.path, required)
				}
			}
		})
	}

	bootstrap, err := os.ReadFile(filepath.Join(roleDir(t, "k0s_controller_bootstrap"), "tasks", "main.yml"))
	if err != nil {
		t.Fatalf("read bootstrap role: %v", err)
	}
	if strings.Contains(string(bootstrap), "--single") {
		t.Error("standalone deployment must remain scale-out capable and not use --single")
	}

	workerJoin, err := os.ReadFile(filepath.Join(roleDir(t, "k0s_worker_join"), "tasks", "main.yml"))
	if err != nil {
		t.Fatalf("read worker join role: %v", err)
	}
	workerJoinContent := string(workerJoin)
	directoryTask := strings.Index(workerJoinContent, "Ensure /etc/k0s exists")
	tokenTask := strings.Index(workerJoinContent, "Write the join token")
	if directoryTask == -1 || tokenTask == -1 || directoryTask > tokenTask {
		t.Error("worker join role must create /etc/k0s before writing its join token")
	}
}

// kubeObject is the part of a Kubernetes manifest the credential role's content test reads.
type kubeObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name        string            `yaml:"name"`
		Namespace   string            `yaml:"namespace"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
	Rules []struct {
		APIGroups       []string `yaml:"apiGroups"`
		Resources       []string `yaml:"resources"`
		NonResourceURLs []string `yaml:"nonResourceURLs"`
		Verbs           []string `yaml:"verbs"`
	} `yaml:"rules"`
	RoleRef struct {
		Kind string `yaml:"kind"`
		Name string `yaml:"name"`
	} `yaml:"roleRef"`
	Subjects []struct {
		Kind      string `yaml:"kind"`
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"subjects"`
	Type string `yaml:"type"`
}

// readCredentialObjects parses one of the credential role's templates, keyed by kind/name. The
// templates hold no Jinja, so what the test reads is what kubectl applies.
func readCredentialObjects(t *testing.T, template string) map[string]kubeObject {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(roleDir(t, "k0s_cluster_credential"), "templates", template))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("{{")) || bytes.Contains(raw, []byte("{%")) {
		t.Fatalf("%s must stay plain YAML so this test reads what kubectl applies", template)
	}
	objects := map[string]kubeObject{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var object kubeObject
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse %s: %v", template, err)
		}
		objects[object.Kind+"/"+object.Metadata.Name] = object
	}
	return objects
}

// checkServiceAccountToken requires the ServiceAccount in kube-system, its binding to role, and the
// long-lived token Secret the role reads back.
func checkServiceAccountToken(t *testing.T, objects map[string]kubeObject, account, binding, role string) {
	t.Helper()
	if sa, ok := objects["ServiceAccount/"+account]; !ok || sa.Metadata.Namespace != "kube-system" {
		t.Errorf("ServiceAccount %s in kube-system missing: %+v", account, sa)
	}
	bound := objects["ClusterRoleBinding/"+binding]
	if bound.RoleRef.Kind != "ClusterRole" || bound.RoleRef.Name != role || len(bound.Subjects) != 1 ||
		bound.Subjects[0].Kind != "ServiceAccount" || bound.Subjects[0].Name != account || bound.Subjects[0].Namespace != "kube-system" {
		t.Errorf("ClusterRoleBinding %s = %+v, want only %s bound to ClusterRole %s", binding, bound, account, role)
	}
	token := objects["Secret/"+account+"-token"]
	if token.Type != "kubernetes.io/service-account-token" || token.Metadata.Annotations["kubernetes.io/service-account.name"] != account {
		t.Errorf("token Secret of %s = %+v", account, token)
	}
}

// The reader token reads exactly what swallow reads (kubeapi's Reader and Client), with no write and
// no Secret, whose list returns values; the explorer's writes get a separate cluster-admin operator
// token; and the reader binding does not reuse the name earlier deployments bound to cluster-admin,
// whose roleRef cannot change, which the role deletes only after both tokens are read.
func TestK0sClusterCredentialSeparatesTheReaderFromTheOperator(t *testing.T) {
	reader := readCredentialObjects(t, "swallow-reader.yaml.j2")
	role, ok := reader["ClusterRole/swallow-reader"]
	if !ok {
		t.Fatal("swallow-reader.yaml.j2 defines no ClusterRole swallow-reader")
	}
	readVerbs := map[string]bool{"get": true, "list": true, "watch": true}
	granted := map[string]bool{}
	for _, rule := range role.Rules {
		for _, verb := range rule.Verbs {
			if !readVerbs[verb] {
				t.Errorf("reader rule %+v grants %q, want only get, list, and watch", rule, verb)
			}
			for _, url := range rule.NonResourceURLs {
				granted[url+":"+verb] = true
			}
			for _, group := range rule.APIGroups {
				for _, resource := range rule.Resources {
					if group == "*" || resource == "*" || strings.HasPrefix(resource, "secrets") {
						t.Errorf("reader rule %+v grants %s/%s", rule, group, resource)
					}
					granted[group+"/"+resource+":"+verb] = true
				}
			}
		}
	}
	for _, read := range []string{
		"/nodes:list",                     // Reader.ListMembers, Client.ListNodes
		"coordination.k8s.io/leases:list", // Reader.controllerMembers
		"/namespaces:list",                // Client.ListNamespaces
		"/pods:list", "/pods:get", "/pods/log:get",
		"apps/deployments:list", "apps/deployments:get",
		"apps/daemonsets:list", "apps/daemonsets:get",
		"apps/statefulsets:list", "apps/statefulsets:get",
		"/services:list", "networking.k8s.io/ingresses:list", "/configmaps:list", "/persistentvolumeclaims:list",
		"resource.k8s.io/resourceslices:list", // Client.draGPUDevicesByNode
		"/version:get",                        // Client.Summary
	} {
		if !granted[read] {
			t.Errorf("the reader ClusterRole does not grant %s", read)
		}
	}

	legacy, _ := readRoleDefaults(t, "k0s_cluster_credential")["k0s_cluster_credential_legacy_reader_binding"].(string)
	if legacy != "swallow-reader" {
		t.Errorf("legacy reader binding = %q, want swallow-reader, the name earlier deployments used", legacy)
	}
	if _, reused := reader["ClusterRoleBinding/"+legacy]; reused {
		t.Errorf("the reader binding reuses %q, whose roleRef on an existing cluster is cluster-admin", legacy)
	}
	checkServiceAccountToken(t, reader, "swallow-reader", "swallow-reader-read-only", "swallow-reader")

	operator := readCredentialObjects(t, "swallow-operator.yaml.j2")
	checkServiceAccountToken(t, operator, "swallow-operator", "swallow-operator", "cluster-admin")

	var tasks []map[string]any
	readYAML(t, filepath.Join(roleDir(t, "k0s_cluster_credential"), "tasks", "main.yml"), &tasks)
	encoded := make([]string, len(tasks))
	for index, task := range tasks {
		raw, err := yaml.Marshal(task)
		if err != nil {
			t.Fatal(err)
		}
		encoded[index] = string(raw)
		if strings.Contains(encoded[index], "token") && task["no_log"] != true {
			t.Errorf("task %q touches a token without no_log: true", task["name"])
		}
	}
	index := func(fragment string) int {
		t.Helper()
		for i, task := range encoded {
			if strings.Contains(task, fragment) {
				return i
			}
		}
		t.Fatalf("no k0s_cluster_credential task contains %q", fragment)
		return -1
	}
	readReader, readOperator := index("get secret swallow-reader-token"), index("get secret swallow-operator-token")
	removeLegacy, writeResult := index("delete clusterrolebinding"), index("swallow_result_path")
	if removeLegacy < readReader || removeLegacy < readOperator || removeLegacy > writeResult {
		t.Errorf("the legacy binding is removed at task %d, want after both token reads (%d, %d) and before the result (%d)",
			removeLegacy, readReader, readOperator, writeResult)
	}
}

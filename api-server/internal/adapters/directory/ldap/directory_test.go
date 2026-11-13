package ldap

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	goldap "github.com/go-ldap/ldap/v3"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

const testBase = "dc=swallow,dc=internal"

func TestUserEntry_AddsThePublicKeyClassOnlyWithKeys(t *testing.T) {
	user := accessdomain.DirectoryUser{
		Username: "alice", UIDNumber: 10001, GIDNumber: 10000, Home: "/home/alice", Shell: "/bin/bash",
		PasswordHash: "$2a$10$hash", Hosts: []string{"node-2", "node-1"},
	}
	got := userEntry(user, testBase)
	want := entry{dn: "uid=alice,ou=people," + testBase, attrs: map[string][]string{
		"objectClass": {"account", "posixAccount", "top"}, "uid": {"alice"}, "cn": {"alice"},
		"uidNumber": {"10001"}, "gidNumber": {"10000"}, "homeDirectory": {"/home/alice"}, "loginShell": {"/bin/bash"},
		"userPassword": {"{CRYPT}$2a$10$hash"}, "host": {"node-1", "node-2"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("userEntry = %+v, want %+v", got, want)
	}

	user.SSHPublicKeys = []string{"ssh-ed25519 AAAA alice@laptop"}
	got = userEntry(user, testBase)
	if !reflect.DeepEqual(got.attrs["objectClass"], []string{"account", "ldapPublicKey", "posixAccount", "top"}) ||
		!reflect.DeepEqual(got.attrs["sshPublicKey"], user.SSHPublicKeys) {
		t.Errorf("with keys: objectClass %v, sshPublicKey %v", got.attrs["objectClass"], got.attrs["sshPublicKey"])
	}
}

func TestSudoEntry_PasswordlessAddsTheOption(t *testing.T) {
	rule := accessdomain.SudoRule{Name: "pool-a-member-nopasswd", Users: []string{"bob"}, Hosts: []string{"node-1", "10.0.0.5"}, Passwordless: true}
	got := sudoEntry(rule, testBase)
	want := entry{dn: "cn=pool-a-member-nopasswd,ou=sudoers," + testBase, attrs: map[string][]string{
		"objectClass": {"sudoRole", "top"}, "cn": {"pool-a-member-nopasswd"}, "sudoCommand": {"ALL"},
		"sudoRunAsUser": {"ALL"}, "sudoRunAsGroup": {"ALL"}, "sudoUser": {"bob"},
		"sudoHost": {"10.0.0.5", "node-1"}, "sudoOption": {"!authenticate"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sudoEntry = %+v, want %+v", got, want)
	}
}

func TestPlan_AddsModifiesAndDeletes(t *testing.T) {
	existing := []entry{
		{dn: "cn=keep,ou=groups," + testBase, attrs: map[string][]string{"cn": {"keep"}, "gidNumber": {"1"}}},
		{dn: "CN=Change,ou=groups," + testBase, attrs: map[string][]string{"cn": {"change"}, "gidNumber": {"2"}, "memberUid": {"old"}}},
		{dn: "cn=gone,ou=groups," + testBase, attrs: map[string][]string{"cn": {"gone"}}},
	}
	desired := []entry{
		{dn: "cn=keep,ou=groups," + testBase, attrs: map[string][]string{"cn": {"keep"}, "gidNumber": {"1"}}},
		{dn: "cn=change,ou=groups," + testBase, attrs: map[string][]string{"cn": {"change"}, "gidNumber": {"3"}}},
		{dn: "cn=new,ou=groups," + testBase, attrs: map[string][]string{"cn": {"new"}}},
	}
	got := plan(existing, desired)
	want := []change{
		{kind: changeModify, dn: "CN=Change,ou=groups," + testBase, replace: map[string][]string{"gidNumber": {"3"}, "memberUid": {}}},
		{kind: changeAdd, dn: "cn=new,ou=groups," + testBase, attrs: map[string][]string{"cn": {"new"}}},
		{kind: changeDelete, dn: "cn=gone,ou=groups," + testBase},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plan = %+v, want %+v", got, want)
	}
}

func TestNew_RefusesAnythingButLDAPS(t *testing.T) {
	for _, cfg := range []Config{
		{URL: "ldap://ldap:389", BaseDN: testBase, BindPassword: "x"},
		{URL: "ldaps://ldap:636", BaseDN: "ou=people", BindPassword: "x"},
		{URL: "ldaps://ldap:636", BaseDN: testBase},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v) accepted an invalid config", cfg)
		}
	}
}

func TestClassify(t *testing.T) {
	rejected := classify(goldap.NewError(goldap.LDAPResultObjectClassViolation, errors.New("missing sshPublicKey")), "add uid=a")
	if !errors.Is(rejected, accessdomain.ErrDirectoryRejected) || !strings.Contains(rejected.Error(), "Object Class Violation") {
		t.Errorf("object class violation = %v, want ErrDirectoryRejected naming the result", rejected)
	}
	unreachable := classify(goldap.NewError(goldap.ErrorNetwork, errors.New("connection reset")), "read ou=people")
	if !errors.Is(unreachable, accessdomain.ErrDirectoryUnreachable) {
		t.Errorf("network error = %v, want ErrDirectoryUnreachable", unreachable)
	}
}

// TestApply_AgainstALiveServer converges a real LDAPS server through two contents and back to an
// empty one. It runs only when SWALLOW_TEST_LDAP_URL, SWALLOW_TEST_LDAP_BIND_PASSWORD_FILE, and
// SWALLOW_TEST_LDAP_CA_FILE point at a disposable server built from deploy/production/ldap.Dockerfile;
// it deletes every entry under swallow's units there.
func TestApply_AgainstALiveServer(t *testing.T) {
	url := os.Getenv("SWALLOW_TEST_LDAP_URL")
	if url == "" {
		t.Skip("SWALLOW_TEST_LDAP_URL is not set")
	}
	password, err := os.ReadFile(os.Getenv("SWALLOW_TEST_LDAP_BIND_PASSWORD_FILE"))
	if err != nil {
		t.Fatalf("read bind password: %v", err)
	}
	caPEM, err := os.ReadFile(os.Getenv("SWALLOW_TEST_LDAP_CA_FILE"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	directory, err := New(Config{URL: url, BaseDN: testBase, BindPassword: strings.TrimSpace(string(password)), RootCAs: pool})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	content := accessdomain.DirectoryContent{
		Users: []accessdomain.DirectoryUser{
			{Username: "alice", UIDNumber: 10001, GIDNumber: 10000, Home: "/home/alice", Shell: "/bin/bash", PasswordHash: "$2a$10$abcdefghijklmnopqrstuuMLv7pRk1Cq1yYx4oZ0nWb7xYzA1B2C3", SSHPublicKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA alice"}, Hosts: []string{"node-1"}},
			{Username: "bob", UIDNumber: 10002, GIDNumber: 10000, Home: "/home/bob", Shell: "/bin/bash"},
		},
		Groups:    []accessdomain.DirectoryGroup{{Name: "swallow-users", GIDNumber: 10000, Members: []string{"alice", "bob"}}},
		SudoRules: []accessdomain.SudoRule{{Name: "pool-a-owner-passwd", Users: []string{"alice"}, Hosts: []string{"node-1"}}},
	}
	if _, err := directory.Apply(ctx, accessdomain.DirectoryContent{}); err != nil {
		t.Fatalf("Apply(empty): %v", err)
	}
	result, err := directory.Apply(ctx, content)
	if err != nil {
		t.Fatalf("Apply(first): %v", err)
	}
	if result != (accessdomain.ApplyResult{Added: 4}) {
		t.Errorf("first apply = %+v, want 4 added", result)
	}
	if result, err = directory.Apply(ctx, content); err != nil || result != (accessdomain.ApplyResult{}) {
		t.Errorf("repeated apply = %+v, %v; want no change", result, err)
	}

	content.Users[0].SSHPublicKeys = nil
	content.Users[1].SSHPublicKeys = []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB bob"}
	content.SudoRules[0].Passwordless = true
	content.Groups[0].Members = []string{"alice"}
	content.Users = content.Users[:2]
	if result, err = directory.Apply(ctx, content); err != nil || result != (accessdomain.ApplyResult{Modified: 4}) {
		t.Errorf("changed apply = %+v, %v; want 4 modified", result, err)
	}
	if result, err = directory.Apply(ctx, accessdomain.DirectoryContent{}); err != nil || result != (accessdomain.ApplyResult{Deleted: 4}) {
		t.Errorf("emptying apply = %+v, %v; want 4 deleted", result, err)
	}
}

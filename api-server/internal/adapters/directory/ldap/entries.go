package ldap

import (
	"slices"
	"strconv"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

// The organizational units swallow owns under the base DN. swallow is the only writer of these
// subtrees: Apply deletes every entry in them that the content does not list.
const (
	ouPeople  = "ou=people"
	ouGroups  = "ou=groups"
	ouSudoers = "ou=sudoers"
)

// entry is one LDAP entry as the adapter compares it: its DN and every attribute it manages, each
// with its values sorted. An attribute absent from attrs is one the entry must not have.
type entry struct {
	dn    string
	attrs map[string][]string
}

// managedAttributes lists, per subtree, the attributes the adapter reads back and compares. An
// attribute outside the list (operational attributes, for example) is never touched.
var managedAttributes = map[string][]string{
	ouPeople:  {"objectClass", "uid", "cn", "uidNumber", "gidNumber", "homeDirectory", "loginShell", "userPassword", "sshPublicKey", "host"},
	ouGroups:  {"objectClass", "cn", "gidNumber", "memberUid"},
	ouSudoers: {"objectClass", "cn", "sudoUser", "sudoHost", "sudoCommand", "sudoRunAsUser", "sudoRunAsGroup", "sudoOption"},
}

// userEntry maps one Directory account onto a POSIX account entry (RFC 2307): structural `account`
// (which allows `host`, the attribute SSSD's host access check reads) with `posixAccount`, plus
// `ldapPublicKey` only when the User has keys, since that class requires `sshPublicKey`. The
// password hash is stored as `{CRYPT}<bcrypt>`, which the hosts' PAM verifies for password sudo.
func userEntry(user accessdomain.DirectoryUser, baseDN string) entry {
	classes := []string{"account", "posixAccount", "top"}
	if len(user.SSHPublicKeys) > 0 {
		classes = append(classes, "ldapPublicKey")
	}
	attrs := map[string][]string{
		"objectClass":   classes,
		"uid":           {user.Username},
		"cn":            {user.Username},
		"uidNumber":     {strconv.Itoa(user.UIDNumber)},
		"gidNumber":     {strconv.Itoa(user.GIDNumber)},
		"homeDirectory": {user.Home},
		"loginShell":    {user.Shell},
	}
	if user.PasswordHash != "" {
		attrs["userPassword"] = []string{"{CRYPT}" + user.PasswordHash}
	}
	setIfAny(attrs, "sshPublicKey", user.SSHPublicKeys)
	setIfAny(attrs, "host", user.Hosts)
	return normalized(entry{dn: "uid=" + escapeRDN(user.Username) + "," + ouPeople + "," + baseDN, attrs: attrs})
}

// groupEntry maps one Directory group onto a `posixGroup` entry with its members' login names.
func groupEntry(group accessdomain.DirectoryGroup, baseDN string) entry {
	attrs := map[string][]string{
		"objectClass": {"posixGroup", "top"},
		"cn":          {group.Name},
		"gidNumber":   {strconv.Itoa(group.GIDNumber)},
	}
	setIfAny(attrs, "memberUid", group.Members)
	return normalized(entry{dn: "cn=" + escapeRDN(group.Name) + "," + ouGroups + "," + baseDN, attrs: attrs})
}

// sudoEntry maps one sudo rule onto a `sudoRole` entry (sudoers.ldap): its Users may run every
// command as any user and group on its Hosts; `!authenticate` makes the rule passwordless.
func sudoEntry(rule accessdomain.SudoRule, baseDN string) entry {
	attrs := map[string][]string{
		"objectClass":    {"sudoRole", "top"},
		"cn":             {rule.Name},
		"sudoCommand":    {"ALL"},
		"sudoRunAsUser":  {"ALL"},
		"sudoRunAsGroup": {"ALL"},
	}
	setIfAny(attrs, "sudoUser", rule.Users)
	setIfAny(attrs, "sudoHost", rule.Hosts)
	if rule.Passwordless {
		attrs["sudoOption"] = []string{"!authenticate"}
	}
	return normalized(entry{dn: "cn=" + escapeRDN(rule.Name) + "," + ouSudoers + "," + baseDN, attrs: attrs})
}

// desiredEntries maps the whole content onto entries, keyed by subtree.
func desiredEntries(content accessdomain.DirectoryContent, baseDN string) map[string][]entry {
	out := map[string][]entry{}
	for _, user := range content.Users {
		out[ouPeople] = append(out[ouPeople], userEntry(user, baseDN))
	}
	for _, group := range content.Groups {
		out[ouGroups] = append(out[ouGroups], groupEntry(group, baseDN))
	}
	for _, rule := range content.SudoRules {
		out[ouSudoers] = append(out[ouSudoers], sudoEntry(rule, baseDN))
	}
	return out
}

// change is one write the adapter sends: an add of a missing entry, a replace of the attributes
// that differ (an empty value list deletes the attribute), or a delete of an entry swallow no
// longer lists.
type change struct {
	kind    changeKind
	dn      string
	attrs   map[string][]string
	replace map[string][]string
}

type changeKind int

const (
	changeAdd changeKind = iota
	changeModify
	changeDelete
)

// plan compares the entries a subtree holds with the entries it must hold and returns the writes
// that make it match: adds and modifies in desired order, then deletes. DNs compare
// case-insensitively, as LDAP does for these attribute types.
func plan(existing, desired []entry) []change {
	current := make(map[string]entry, len(existing))
	for _, e := range existing {
		current[foldDN(e.dn)] = e
	}
	wanted := make(map[string]bool, len(desired))
	var changes []change
	for _, want := range desired {
		key := foldDN(want.dn)
		wanted[key] = true
		have, ok := current[key]
		if !ok {
			changes = append(changes, change{kind: changeAdd, dn: want.dn, attrs: want.attrs})
			continue
		}
		replace := map[string][]string{}
		for name, values := range want.attrs {
			if !slices.Equal(values, have.attrs[name]) {
				replace[name] = values
			}
		}
		for name := range have.attrs {
			if _, ok := want.attrs[name]; !ok {
				replace[name] = []string{}
			}
		}
		if len(replace) > 0 {
			changes = append(changes, change{kind: changeModify, dn: have.dn, replace: replace})
		}
	}
	for _, have := range existing {
		if !wanted[foldDN(have.dn)] {
			changes = append(changes, change{kind: changeDelete, dn: have.dn})
		}
	}
	return changes
}

// normalized sorts every attribute's values, so equal entries compare equal however they were
// built or returned by the server.
func normalized(e entry) entry {
	for name, values := range e.attrs {
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		e.attrs[name] = slices.Compact(sorted)
	}
	return e
}

func setIfAny(attrs map[string][]string, name string, values []string) {
	if len(values) > 0 {
		attrs[name] = values
	}
}

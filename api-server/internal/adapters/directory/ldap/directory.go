// Package ldap is the LDAP Directory provider adapter (decision 082; glossary Directory). It makes
// an LDAPv3 server hold exactly the Directory content swallow computes: POSIX accounts with their
// SSH public keys and Host Access, POSIX groups, and sudo rules, under three organizational units
// swallow owns. It speaks only LDAPS and binds as the Directory's writer account; the Servers'
// directory client binds separately as a read-only account the server keeps from reading password
// hashes.
package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

// defaultTimeout bounds dialing and each request when Config.Timeout is zero.
const defaultTimeout = 15 * time.Second

// Config is how the adapter reaches and authenticates to the Directory server.
type Config struct {
	// URL is the server's LDAPS URL (ldaps://host:port); any other scheme is refused, so a
	// password never crosses the network in clear.
	URL string
	// BaseDN is the Directory's base DN, for example dc=swallow,dc=internal.
	BaseDN string
	// BindPassword is the password of the writer account cn=admin,<BaseDN>.
	BindPassword string
	// RootCAs verifies the server certificate (the installation CA); nil uses the system pool.
	RootCAs *x509.CertPool
	// Timeout bounds dialing and each request; zero means defaultTimeout.
	Timeout time.Duration
}

// Directory implements accessdomain.Directory over one LDAPv3 server. It holds no connection
// between calls: each Apply dials, binds, writes, and closes, so a restarted server or rotated
// credential needs nothing but the next pass.
type Directory struct {
	cfg  Config
	host string
}

var _ accessdomain.Directory = (*Directory)(nil)

// New validates cfg and returns the adapter. It does not contact the server.
func New(cfg Config) (*Directory, error) {
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Scheme != "ldaps" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("directory URL must be ldaps://host[:port], got %q", cfg.URL)
	}
	if !strings.HasPrefix(strings.ToLower(cfg.BaseDN), "dc=") {
		return nil, fmt.Errorf("directory base DN must start with dc=, got %q", cfg.BaseDN)
	}
	if cfg.BindPassword == "" {
		return nil, errors.New("directory bind password is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Directory{cfg: cfg, host: parsed.Hostname()}, nil
}

// Apply implements accessdomain.Directory. It ensures swallow's organizational units exist, then,
// per unit, reads the entries it holds, compares them with the content, and writes the difference:
// adds and modifies first, deletes last, so an account never loses its groups or sudo rules before
// it is gone itself. A failed connection, bind, or read wraps ErrDirectoryUnreachable when the
// network failed and ErrDirectoryRejected when the server answered with an error; a refused write
// wraps ErrDirectoryRejected and names the entry. Messages never carry a credential or a hash.
func (d *Directory) Apply(ctx context.Context, content accessdomain.DirectoryContent) (accessdomain.ApplyResult, error) {
	var result accessdomain.ApplyResult
	conn, err := d.connect(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()

	if err := d.ensureUnits(conn); err != nil {
		return result, err
	}
	desired := desiredEntries(content, d.cfg.BaseDN)
	var deletes []change
	for _, unit := range []string{ouPeople, ouGroups, ouSudoers} {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("%w: %v", accessdomain.ErrDirectoryUnreachable, err)
		}
		existing, err := d.read(conn, unit)
		if err != nil {
			return result, err
		}
		for _, c := range plan(existing, desired[unit]) {
			if c.kind == changeDelete {
				deletes = append(deletes, c)
				continue
			}
			if err := d.write(conn, c); err != nil {
				return result, err
			}
			if c.kind == changeAdd {
				result.Added++
			} else {
				result.Modified++
			}
		}
	}
	for _, c := range deletes {
		if err := d.write(conn, c); err != nil {
			return result, err
		}
		result.Deleted++
	}
	return result, nil
}

// connect dials the server over TLS, verifying its certificate against the configured CA and the
// URL's host name, and binds as the writer account.
func (d *Directory) connect(ctx context.Context) (*goldap.Conn, error) {
	dialer := &net.Dialer{Timeout: d.cfg.Timeout}
	tlsConfig := &tls.Config{RootCAs: d.cfg.RootCAs, ServerName: d.host, MinVersion: tls.VersionTLS12}
	conn, err := goldap.DialURL(d.cfg.URL, goldap.DialWithDialer(dialer), goldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("%w: connect to %s: %v", accessdomain.ErrDirectoryUnreachable, d.cfg.URL, err)
	}
	conn.SetTimeout(d.cfg.Timeout)
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < d.cfg.Timeout {
		conn.SetTimeout(time.Until(deadline))
	}
	if err := conn.Bind("cn=admin,"+d.cfg.BaseDN, d.cfg.BindPassword); err != nil {
		conn.Close()
		return nil, classify(err, "bind as the directory writer")
	}
	return conn, nil
}

// ensureUnits creates any of swallow's organizational units that is missing (the bundled server
// creates them on first start; another server may not have them).
func (d *Directory) ensureUnits(conn *goldap.Conn) error {
	for _, unit := range []string{ouPeople, ouGroups, ouSudoers} {
		request := goldap.NewAddRequest(unit+","+d.cfg.BaseDN, nil)
		request.Attribute("objectClass", []string{"organizationalUnit", "top"})
		request.Attribute("ou", []string{strings.TrimPrefix(unit, "ou=")})
		if err := conn.Add(request); err != nil && !goldap.IsErrorWithCode(err, goldap.LDAPResultEntryAlreadyExists) {
			return classify(err, "create "+unit)
		}
	}
	return nil
}

// read returns every entry directly under one of swallow's units, with the attributes the adapter
// manages there.
func (d *Directory) read(conn *goldap.Conn, unit string) ([]entry, error) {
	attributes := managedAttributes[unit]
	request := goldap.NewSearchRequest(
		unit+","+d.cfg.BaseDN, goldap.ScopeSingleLevel, goldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=*)", attributes, nil,
	)
	response, err := conn.SearchWithPaging(request, 500)
	if err != nil {
		return nil, classify(err, "read "+unit)
	}
	entries := make([]entry, 0, len(response.Entries))
	for _, found := range response.Entries {
		e := entry{dn: found.DN, attrs: map[string][]string{}}
		for _, attribute := range found.Attributes {
			if len(attribute.Values) > 0 {
				e.attrs[canonicalAttribute(attributes, attribute.Name)] = attribute.Values
			}
		}
		entries = append(entries, normalized(e))
	}
	return entries, nil
}

// write sends one change.
func (d *Directory) write(conn *goldap.Conn, c change) error {
	switch c.kind {
	case changeAdd:
		request := goldap.NewAddRequest(c.dn, nil)
		for name, values := range c.attrs {
			request.Attribute(name, values)
		}
		if err := conn.Add(request); err != nil {
			return classify(err, "add "+c.dn)
		}
	case changeModify:
		request := goldap.NewModifyRequest(c.dn, nil)
		for name, values := range c.replace {
			request.Replace(name, values)
		}
		if err := conn.Modify(request); err != nil {
			return classify(err, "modify "+c.dn)
		}
	case changeDelete:
		if err := conn.Del(goldap.NewDelRequest(c.dn, nil)); err != nil && !goldap.IsErrorWithCode(err, goldap.LDAPResultNoSuchObject) {
			return classify(err, "delete "+c.dn)
		}
	}
	return nil
}

// classify wraps an LDAP error as unreachable (the connection failed or timed out) or rejected (the
// server answered with an error result). go-ldap's message carries the result code and the server's
// diagnostic, never the request's attribute values.
func classify(err error, what string) error {
	var ldapErr *goldap.Error
	if errors.As(err, &ldapErr) && ldapErr.ResultCode != goldap.ErrorNetwork && ldapErr.ResultCode != goldap.LDAPResultTimeout {
		return fmt.Errorf("%w: %s: %s", accessdomain.ErrDirectoryRejected, what, goldap.LDAPResultCodeMap[ldapErr.ResultCode])
	}
	return fmt.Errorf("%w: %s: %v", accessdomain.ErrDirectoryUnreachable, what, err)
}

// canonicalAttribute returns the managed spelling of an attribute name the server returned, since
// LDAP attribute names are case-insensitive.
func canonicalAttribute(managed []string, name string) string {
	for _, candidate := range managed {
		if strings.EqualFold(candidate, name) {
			return candidate
		}
	}
	return name
}

// escapeRDN escapes an attribute value for use in an RDN (RFC 4514). Usernames, group names, and
// sudo rule names are already restricted to safe characters; this guards the adapter regardless.
func escapeRDN(value string) string {
	var b strings.Builder
	for i, r := range value {
		switch {
		case strings.ContainsRune(`,+"\<>;=`, r), r == '#' && i == 0, r == ' ' && (i == 0 || i == len(value)-1):
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// foldDN lowercases a DN and drops the spaces after RDN separators, enough to compare the DNs this
// adapter writes with the ones the server returns.
func foldDN(dn string) string {
	return strings.ReplaceAll(strings.ToLower(dn), ", ", ",")
}

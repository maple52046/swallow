// Package redact removes secrets from text that leaves the process that produced it — a Task
// diagnostic stored in the workflow store, a timeline event, a structured log line — while keeping
// the rest of the text readable, so a cause stays diagnosable after its credentials are gone.
//
// Any api-server package may depend on it: the core, provider adapters, modules, and the
// composition root. It recognizes only generic secret shapes that any system emits (URL userinfo,
// URL query values, bearer tokens, JSON Web Tokens, Authorization headers, credential key-value
// pairs, PEM private key blocks). It must not hold a rule that knows one vendor's, provider's,
// platform's, or component's own secret format: such a rule belongs to the adapter or module that
// knows the format, which applies it in addition to this package.
//
// Every function is pure, safe for concurrent use, and idempotent: redacting already redacted text
// returns it unchanged, so a value may pass through several layers that each redact it. The
// result is for people to read; it is not a value to send back to the system it came from (a
// redacted URL no longer authenticates, and its masked query is not percent-encoded).
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

// Mask replaces each secret value. It is bracketed so a reader sees that a value was removed
// rather than empty, and every pattern below accepts it as a whole value, so masking it again
// changes nothing.
const Mask = "[REDACTED]"

// privateKeyMask replaces a whole PEM private key block, including its armor lines.
const privateKeyMask = "[REDACTED PRIVATE KEY]"

// credentialWords are the key fragments that mark a key-value pair as a credential. A key matches
// when it contains one of them in any case, so "becomePassword", "X-Api-Key", and "client_secret"
// all match.
const credentialWords = `password|passwd|passphrase|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential`

// maskedValue is the first alternative of every value pattern, so a value an earlier pass masked
// is matched whole instead of up to its closing bracket.
const maskedValue = `\[REDACTED\]`

var (
	// privateKeyBlock matches a PEM private key (RSA, EC, OpenSSH, PKCS#8, PGP) through its END
	// line, or through the end of the text when the block was cut off before it.
	privateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY[A-Z0-9 ]*-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY[A-Z0-9 ]*-----|\z)`)
	// authorizationHeader matches an Authorization or Proxy-Authorization header or field; its
	// value is a quoted string, or the rest of the line up to a comma or semicolon.
	authorizationHeader = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization["']?\s*[:=]\s*)(` + maskedValue + `|"[^"]*"|'[^']*'|[^\r\n,;]+)`)
	// bearerToken matches a bearer token outside an Authorization header (in an error string, a
	// command line). The token must be at least eight characters, so prose such as
	// "missing bearer token" keeps its words.
	bearerToken = regexp.MustCompile(`(?i)\b(bearer\s+)([A-Za-z0-9\-._~+/]{8,}=*)`)
	// jsonWebToken matches a JWT (three base64url segments, the first a JSON header), the form of
	// most API tokens a kubeconfig or an OAuth flow carries.
	jsonWebToken = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	// urlUserinfo matches the userinfo of a URL (scheme://user:password@).
	urlUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)([^/\s@]+)@`)
	// assignedCredential matches key=value where the key names a credential, anywhere in the text:
	// a query parameter, a command-line option, a log field.
	assignedCredential = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:` + credentialWords + `)[A-Za-z0-9_.-]*\s*=\s*)(` +
		maskedValue + `|"[^"]*"|'[^']*'|[^\s&,;"'}\]]+)`)
	// quotedCredential matches a quoted key naming a credential followed by a colon and its value,
	// as in JSON ("password": "x") or a Python dict ('token': 'x').
	quotedCredential = regexp.MustCompile(`(?i)(["'][A-Za-z0-9_.-]*(?:` + credentialWords + `)[A-Za-z0-9_.-]*["']\s*:\s*)(` +
		maskedValue + `|"(?:[^"\\]|\\.)*"|'[^']*'|[^\s,}\]]+)`)
	// lineCredential matches a line that starts with a key naming a credential followed by a colon
	// (a YAML field, an HTTP header such as X-Api-Key), masking the rest of the line. A colon in
	// the middle of a sentence ("invalid token: expired") is left alone, because that is a cause,
	// not a value.
	lineCredential = regexp.MustCompile(`(?im)^(\s*-?\s*[A-Za-z0-9_.-]*(?:` + credentialWords + `)[A-Za-z0-9_.-]*\s*:[ \t]+)(\S[^\r\n]*)$`)
)

// Text returns text with every recognized secret replaced by Mask: PEM private key blocks,
// Authorization header values, bearer tokens, JSON Web Tokens, URL userinfo, and the values of
// credential key-value pairs (key=value anywhere, a quoted key with a colon, and a "key: value"
// line). A quoted value keeps its quotes. Words around a secret are kept, so "password
// authentication failed for user root" and "dial tcp 10.0.0.5:6443: connect: connection refused"
// read as before. Detection is by shape, so a secret in an unrecognized shape (a bare random
// string with no key) passes through: callers must not put credentials into a message on purpose
// and rely on this to remove them.
func Text(text string) string {
	if text == "" {
		return ""
	}
	text = privateKeyBlock.ReplaceAllString(text, privateKeyMask)
	text = maskValues(authorizationHeader, text)
	text = maskValues(bearerToken, text)
	text = jsonWebToken.ReplaceAllString(text, Mask)
	text = maskValues(urlUserinfo, text)
	text = maskValues(assignedCredential, text)
	text = maskValues(quotedCredential, text)
	return maskValues(lineCredential, text)
}

// Error returns the redacted text of err, or "" for a nil error. It is the form an executor puts
// into a diagnostic's cause: the whole wrapped chain ("list nodes: Get https://...: dial tcp ...:
// connection refused") stays readable, without the credentials any link of it carried.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}

// URL returns raw as an endpoint safe to show: the userinfo is dropped, every query value (and
// every query part without a value) is replaced by Mask with the keys kept in order, the fragment
// is dropped, and the result passes through Text. A value that is not an absolute URL (a bare
// host:port, an address) is redacted as Text, so it is returned unchanged unless it holds a
// recognized secret.
func URL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return Text(trimmed)
	}
	parsed.User = nil
	parsed.Fragment, parsed.RawFragment = "", ""
	parsed.ForceQuery = false
	parsed.RawQuery = maskQuery(parsed.RawQuery)
	return Text(parsed.String())
}

// maskValues replaces the last capture group of every match of re — the secret value — with Mask,
// keeping the text before it (the key and separator) and the quotes of a quoted value. Punctuation
// that ends an unquoted value belongs to the sentence around it, so it is kept: a colon or period
// followed by whitespace or the end of the text (the separator of a wrapped error chain,
// "...?token=abc: 401 Unauthorized", or the end of a sentence), and a closing parenthesis the value
// did not open. Keeping it can show at most that punctuation of a secret that really ended in it.
func maskValues(re *regexp.Regexp, text string) string {
	matches := re.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	last := 0
	for _, match := range matches {
		start, end := match[len(match)-2], match[len(match)-1]
		for trimmed := false; end-start > 1; trimmed = true {
			c := text[end-1]
			boundary := trimmed || end == len(text) || strings.IndexByte(" \t\r\n", text[end]) >= 0
			if !((c == ':' || c == '.') && boundary) && !(c == ')' && !strings.Contains(text[start:end-1], "(")) {
				break
			}
			end--
		}
		out.WriteString(text[last:start])
		out.WriteString(maskOf(text[start:end]))
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// maskOf is the replacement of one secret value: Mask, inside the value's own quotes when it was
// quoted, so a redacted JSON document stays well formed.
func maskOf(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[:1] + Mask + value[:1]
	}
	return Mask
}

// maskQuery replaces every value of a raw query with Mask, keeping each key and the order of the
// parts. A part without "=" may itself be a secret (a bare token), so it is masked whole.
func maskQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, part := range parts {
		if part == "" {
			continue
		}
		key, _, found := strings.Cut(part, "=")
		if !found {
			parts[i] = Mask
			continue
		}
		parts[i] = key + "=" + Mask
	}
	return strings.Join(parts, "&")
}

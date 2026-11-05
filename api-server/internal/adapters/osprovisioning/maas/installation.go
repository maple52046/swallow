package maas

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maple52046/swallow/internal/shared/redact"
)

const (
	// maxInstallationLogBytes bounds the downloaded installation log; curtin's install.log is
	// tens of kilobytes, and its errors are near the end of the partitioning stage.
	maxInstallationLogBytes = 4 << 20
	// maxInstallationErrorRunes bounds the line carried into a Step error message.
	maxInstallationErrorRunes = 300
	// scriptResultTimeLayout is the form MAAS uses for script set times,
	// for example "Mon, 19 Oct 2026 05:28:32 +0000".
	scriptResultTimeLayout = "Mon, 02 Jan 2006 15:04:05 -0700"
)

// installationResultSetJSON is one MAAS installation script set. Its results include the
// text install.log and a binary curtin-logs.tar.
type installationResultSetJSON struct {
	ID         int    `json:"id"`
	StatusName string `json:"status_name"`
	Started    string `json:"started"`
	Results    []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"results"`
}

var (
	// pythonExceptionLine matches the final line of a Python traceback, such as
	// "RuntimeError: Cannot find previous partition on disk /dev/nvme0n1".
	pythonExceptionLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*(Error|Exception): \S`)
	// maasAPIKey matches a MAAS API key, consumer key, token key, and token secret joined by
	// colons, which has no key name the shared redaction could recognize it by.
	maasAPIKey = regexp.MustCompile(`\b[A-Za-z0-9]{18,}:[A-Za-z0-9]{18,}:[A-Za-z0-9]{32,}\b`)
	// maasOAuthValue matches the OAuth parameters of a MAAS request (as curtin's reporting
	// configuration or a logged header carries them) whose names hold no credential word: the
	// consumer key, and the PLAINTEXT signature, which is the token secret itself.
	maasOAuthValue = regexp.MustCompile(`(?i)\b((?:oauth_)?(?:consumer_key|signature)["']?\s*[:=]\s*)(\[REDACTED\]|"[^"]*"|'[^']*'|[^\s,;"'}\]]+)`)
)

// LatestInstallationFailure reads the newest failed MAAS installation that started at or after
// since and returns the most specific error line of its install.log. MAAS does not order script
// sets, so the newest one has the highest ID. Only the text log is downloaded, never the binary
// curtin log archive.
func (p *Provider) LatestInstallationFailure(ctx context.Context, machineID string, since time.Time) (string, error) {
	resultsPath := "/nodes/" + url.PathEscape(machineID) + "/results/"
	var sets []installationResultSetJSON
	if err := p.client.get(ctx, resultsPath, url.Values{"type": {"installation"}}, &sets); err != nil {
		return "", translateError(err, machineID)
	}

	var latest *installationResultSetJSON
	for index := range sets {
		set := &sets[index]
		if !strings.EqualFold(set.StatusName, "Failed") || (latest != nil && set.ID <= latest.ID) {
			continue
		}
		if !since.IsZero() {
			started, err := time.Parse(scriptResultTimeLayout, strings.TrimSpace(set.Started))
			if err != nil || started.Before(since) {
				continue
			}
		}
		latest = set
	}
	if latest == nil {
		return "", nil
	}
	logID := 0
	for _, result := range latest.Results {
		if path.Base(result.Name) == "install.log" {
			logID = result.ID
			break
		}
	}
	if logID == 0 {
		return "", nil
	}

	query := url.Values{}
	query.Set("op", "download")
	query.Set("output", "combined")
	query.Set("filetype", "txt")
	query.Set("filters", strconv.Itoa(logID))
	log, err := p.client.getText(ctx, resultsPath+strconv.Itoa(latest.ID)+"/", query, maxInstallationLogBytes)
	if err != nil {
		return "", translateError(err, machineID)
	}
	return installationErrorLine(log), nil
}

// installationErrorLine picks the most specific error line of a curtin installation log: the
// storage or stage handler error curtin reports first, then the exception that ended its
// traceback, then the first failed stage. Every line is redacted before it is considered, so a
// line that mentions a credential keeps its cause with the secret masked (decision 077).
func installationErrorLine(log string) string {
	exception, failure := "", ""
	for _, raw := range strings.Split(log, "\n") {
		line := redactInstallationLine(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		switch {
		case strings.Contains(line, "An error occured handling"), strings.Contains(line, "An error occurred handling"):
			return boundInstallationError(line)
		case exception == "" && pythonExceptionLine.MatchString(line):
			exception = line
		case failure == "" && strings.Contains(line, "FAIL: "):
			failure = line
		}
	}
	if exception != "" {
		return boundInstallationError(exception)
	}
	return boundInstallationError(failure)
}

// redactInstallationLine masks the secrets of one installation log line: first the MAAS shapes
// only this adapter knows (maasAPIKey, maasOAuthValue), then every generic shape (redact.Text:
// URL userinfo, tokens, credential key-value pairs). The line is redacted before it is bounded,
// so a cut never leaves a secret without the key that marked it.
func redactInstallationLine(line string) string {
	line = maasAPIKey.ReplaceAllString(line, redact.Mask)
	line = maasOAuthValue.ReplaceAllString(line, "${1}"+redact.Mask)
	return redact.Text(line)
}

func boundInstallationError(line string) string {
	if utf8.RuneCountInString(line) <= maxInstallationErrorRunes {
		return line
	}
	runes := []rune(line)
	return string(runes[:maxInstallationErrorRunes-3]) + "..."
}

package maas

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

type eventsJSON struct {
	Events []eventJSON `json:"events"`
}

type eventJSON struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Level       string `json:"level"`
	Created     string `json:"created"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ListMachineEvents reads MAAS's provider-owned event stream for one system ID. MAAS
// returns newest events first, and its documented query supports up to 1000; the Swallow
// delivery contract intentionally caps the operator view at 100.
func (p *Provider) ListMachineEvents(
	ctx context.Context,
	machineID string,
	limit int,
) ([]provisioningdomain.MachineEvent, error) {
	query := url.Values{}
	query.Set("op", "query")
	query.Set("id", machineID)
	query.Set("limit", strconv.Itoa(limit))

	var out eventsJSON
	if err := p.client.get(ctx, "/events/", query, &out); err != nil {
		return nil, translateError(err, "")
	}

	events := make([]provisioningdomain.MachineEvent, 0, len(out.Events))
	for _, event := range out.Events {
		events = append(events, provisioningdomain.MachineEvent{
			ID:          strconv.FormatInt(event.ID, 10),
			Level:       strings.ToLower(event.Level),
			Type:        event.Type,
			Description: event.Description,
			Actor:       event.Username,
			OccurredAt:  normalizeEventTime(event.Created),
		})
	}
	return events, nil
}

// normalizeEventTime handles both the RFC3339 form and the human-readable UTC form
// returned by current MAAS event queries. Unknown values are retained so one unusual
// event cannot hide the rest of the provider history.
func normalizeEventTime(value string) string {
	trimmed := strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"Mon, 02 Jan. 2006 15:04:05",
		"Mon, 2 Jan. 2006 15:04:05",
	} {
		parsed, err := time.ParseInLocation(layout, trimmed, time.UTC)
		if err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return trimmed
}

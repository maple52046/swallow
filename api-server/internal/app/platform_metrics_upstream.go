package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maple52046/swallow/internal/adapters/metrics/promstack"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// platformMetricsUpstream satisfies the monitoring context's PlatformMetricsUpstream port: a
// Platform's metrics are read through the label proxy its Site's metrics Integration names.
type platformMetricsUpstream struct {
	platforms    platformFinder
	integrations sitedomain.IntegrationRepository
}

// LabelProxyURL returns the label proxy of the Platform's Site.
func (u platformMetricsUpstream) LabelProxyURL(ctx context.Context, platformID string) (string, error) {
	platform, err := u.platforms.FindByID(ctx, platformID)
	if errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return "", fmt.Errorf("%w: platform %q", monitoringdomain.ErrScopeNotFound, platformID)
	}
	if err != nil {
		return "", err
	}
	integrations, err := u.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID: platform.SiteID, Kind: sitedomain.IntegrationKindMetrics, EnabledOnly: true,
	})
	if err != nil {
		return "", err
	}
	for _, integration := range integrations {
		if proxy := strings.TrimSpace(integration.Setting(promstack.SettingLabelProxyURL, "")); proxy != "" {
			return proxy, nil
		}
	}
	return "", monitoringdomain.ErrNoMetricsIntegration
}

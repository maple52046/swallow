package awx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

const (
	controllerName = "awx"
	// timeLayout is the timestamp format AWX emits.
	timeLayout = time.RFC3339
)

// Controller implements operationdomain.AutomationController against AWX.
type Controller struct {
	client *Client
}

func NewController(client *Client) *Controller {
	return &Controller{client: client}
}

func (c *Controller) Name() string { return controllerName }

type jobTemplateListJSON struct {
	Count   int `json:"count"`
	Results []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"results"`
}

// FindJobTemplate resolves a template name to its AWX ID.
//
// An exact-name match is required: AWX's name filter is a substring search, so
// accepting the first result would happily launch "deploy-kubernetes-staging" when
// asked for "deploy-kubernetes".
func (c *Controller) FindJobTemplate(ctx context.Context, name string) (string, error) {
	query := url.Values{}
	query.Set("name", name)

	var out jobTemplateListJSON
	if err := c.client.get(ctx, "/job_templates/", query, &out); err != nil {
		return "", translateError(err)
	}

	for _, result := range out.Results {
		if result.Name == name {
			return strconv.Itoa(result.ID), nil
		}
	}
	return "", fmt.Errorf("%w: %q", operationdomain.ErrJobTemplateNotFound, name)
}

type launchResponseJSON struct {
	Job     int    `json:"job"`
	ID      int    `json:"id"`
	Status  string `json:"status"`
	Started string `json:"started"`
}

func (c *Controller) Launch(
	ctx context.Context,
	req operationdomain.LaunchRequest,
) (string, operationdomain.JobState, error) {
	payload := map[string]any{}
	if len(req.Limit) > 0 {
		payload["limit"] = strings.Join(req.Limit, ",")
	}
	if len(req.ExtraVars) > 0 {
		payload["extra_vars"] = req.ExtraVars
	}

	var out launchResponseJSON
	path := "/job_templates/" + url.PathEscape(req.JobTemplateID) + "/launch/"
	if err := c.client.postJSON(ctx, path, payload, &out); err != nil {
		return "", operationdomain.JobState{}, translateError(err)
	}

	// AWX reports the new job's id as "job" on this endpoint and as "id" elsewhere.
	jobID := out.Job
	if jobID == 0 {
		jobID = out.ID
	}
	if jobID == 0 {
		return "", operationdomain.JobState{}, &operationdomain.ControllerError{
			Kind:   operationdomain.ControllerErrorUnavailable,
			Detail: "AWX accepted the launch but did not return a job id.",
		}
	}

	return strconv.Itoa(jobID), operationdomain.JobState{
		Status:    mapStatus(out.Status),
		StartedAt: parseTime(out.Started),
	}, nil
}

type jobJSON struct {
	ID       int    `json:"id"`
	Status   string `json:"status"`
	Started  string `json:"started"`
	Finished string `json:"finished"`
}

func (c *Controller) JobState(ctx context.Context, jobID string) (operationdomain.JobState, error) {
	var out jobJSON
	if err := c.client.get(ctx, "/jobs/"+url.PathEscape(jobID)+"/", nil, &out); err != nil {
		return operationdomain.JobState{}, translateError(err)
	}

	return operationdomain.JobState{
		Status:     mapStatus(out.Status),
		StartedAt:  parseTime(out.Started),
		FinishedAt: parseTime(out.Finished),
	}, nil
}

func (c *Controller) JobLogs(ctx context.Context, jobID string) (string, error) {
	query := url.Values{}
	query.Set("format", "txt")

	logs, err := c.client.getText(ctx, "/jobs/"+url.PathEscape(jobID)+"/stdout/", query)
	if err != nil {
		return "", translateError(err)
	}
	return logs, nil
}

// mapStatus collapses AWX job statuses onto the domain set.
//
// Every pre-run state becomes pending: swallow has no use for the difference between
// queued and waiting for a capacity slot. An unrecognised status becomes pending rather
// than an outcome, because guessing that an unknown state is a failure would be worse
// than waiting for the next poll.
func mapStatus(awxStatus string) operationdomain.Status {
	switch strings.ToLower(awxStatus) {
	case "new", "pending", "waiting":
		return operationdomain.StatusPending
	case "running":
		return operationdomain.StatusRunning
	case "successful":
		return operationdomain.StatusSucceeded
	case "failed":
		return operationdomain.StatusFailed
	case "canceled", "cancelled":
		return operationdomain.StatusCanceled
	case "error":
		return operationdomain.StatusError
	default:
		return operationdomain.StatusPending
	}
}

func parseTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(timeLayout, raw)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

// ErrJobNotFound means AWX no longer has the job. Distinguished from other failures so
// that a vanished job becomes indeterminate rather than failed.
var ErrJobNotFound = errors.New("awx job not found")

func translateError(err error) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &operationdomain.ControllerError{
			Kind:   operationdomain.ControllerErrorUnavailable,
			Detail: "Could not reach AWX.",
			Err:    err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return err
	}

	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		return ErrJobNotFound

	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		return &operationdomain.ControllerError{
			Kind:   operationdomain.ControllerErrorAuth,
			Detail: "AWX rejected the token swallow is configured with.",
			Err:    err,
		}

	case apiErr.StatusCode >= http.StatusInternalServerError:
		return &operationdomain.ControllerError{
			Kind:   operationdomain.ControllerErrorUnavailable,
			Detail: fmt.Sprintf("AWX reported an internal error (HTTP %d).", apiErr.StatusCode),
			Err:    err,
		}

	default:
		detail := apiErr.Body
		if detail == "" {
			detail = fmt.Sprintf("AWX refused the request (HTTP %d).", apiErr.StatusCode)
		} else {
			detail = "AWX refused the request: " + detail
		}
		return &operationdomain.ControllerError{
			Kind:   operationdomain.ControllerErrorRejected,
			Detail: detail,
			Err:    err,
		}
	}
}

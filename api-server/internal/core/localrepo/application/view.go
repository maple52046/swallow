package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// SourceView is one effective Repository Source with what the controller last observed of it. An
// item not observed since its definition last changed is pending with no message.
type SourceView struct {
	EffectiveSource
	State        localrepodomain.SyncState
	Message      string
	LastSyncedAt *time.Time
	ContentCount int
}

// FileView is one effective File with what the controller last observed of it.
type FileView struct {
	EffectiveFile
	State     localrepodomain.SyncState
	Message   string
	SizeBytes int64
}

// ImageView is one declared image with what the controller last observed of it; an image the status
// does not hold (never observed, or a reference declared since) is pending with no message.
type ImageView struct {
	EffectiveImage
	State     localrepodomain.SyncState
	Message   string
	SizeBytes int64
}

// View is a Site's Local Repository as the API and the deployment resolver read it: the user's spec,
// the effective spec joined with the recorded status, the controller's conditions, and whether the
// repository is ready. It is built from the store alone and never calls the provider.
//
// Ready means the Site's repository Integration is enabled, the controller's last pass on it was
// not stopped by the Integration or its provider, every effective source, file, and declared image
// is ready, and its addresses were read; otherwise NotReady lists the causes in that order, each with
// a condition reason and a sentence telling a user what to fix, enable, or wait for.
type View struct {
	SiteID             string
	Integration        *sitedomain.Integration
	Generation         int64
	Spec               localrepodomain.Spec
	Sources            []SourceView
	Files              []FileView
	Images             []ImageView
	Python             []EffectivePython
	Endpoints          *localrepodomain.HostEndpoints
	ObservedAt         *time.Time
	ObservedGeneration int64
	Conditions         []convergence.Condition
	Ready              bool
	NotReady           []NotReadyCause
}

// NotReadyCause is one thing that keeps a Local Repository from being ready: Reason is a
// localrepodomain or convergence condition reason, Message the sentence for a user.
type NotReadyCause struct {
	Reason  string
	Message string
}

// NotReadyMessage joins the causes' sentences.
func (v View) NotReadyMessage() string {
	messages := make([]string, 0, len(v.NotReady))
	for _, cause := range v.NotReady {
		messages = append(messages, cause.Message)
	}
	return strings.Join(messages, "; ")
}

// buildView joins the effective spec with record's status. record may be nil (nothing written or
// observed yet); integration is the Site's repository Integration and not nil. A status recorded for
// another Integration is ignored.
func buildView(siteID string, integration *sitedomain.Integration, record *localrepodomain.LocalRepository, effective EffectiveSpec) View {
	view := View{SiteID: siteID, Integration: integration, Python: effective.Python}
	var status localrepodomain.Status
	if record != nil {
		view.Generation, view.Spec = record.Generation, record.Spec
		if record.Status.IntegrationID == integration.ID {
			status = record.Status
		}
	}
	view.Endpoints, view.ObservedAt = status.Endpoints, status.ObservedAt
	view.ObservedGeneration, view.Conditions = status.ObservedGeneration, status.Conditions
	cause := func(reason, message string) {
		view.NotReady = append(view.NotReady, NotReadyCause{Reason: reason, Message: message})
	}
	if !integration.Enabled {
		cause(localrepodomain.ReasonIntegrationDisabled, fmt.Sprintf("its repository integration %s is disabled; enable it", integration.Name))
	}
	blocked := false
	if realized, ok := convergence.FindCondition(status.Conditions, convergence.ConditionRealized); ok &&
		realized.Status == convergence.ConditionFalse && localrepodomain.ConfigurationReason(realized.Reason) {
		cause(realized.Reason, realized.Message)
		blocked = true
	}
	observed := status.ObservedAt != nil
	if !observed && !blocked {
		cause(localrepodomain.ReasonNotObserved, "it has not been observed yet; wait for the worker to converge it")
	}
	for _, source := range effective.Sources {
		item := SourceView{EffectiveSource: source, State: localrepodomain.SyncPending}
		if found, ok := observedSource(status, source.Source); ok {
			item.State, item.Message = found.State, found.Message
			item.LastSyncedAt, item.ContentCount = found.LastSyncedAt, found.ContentCount
		}
		if item.State != localrepodomain.SyncReady && observed {
			cause(itemReason(item.State, localrepodomain.ReasonSourceFailed), notReady("source "+source.Name, item.State, item.Message))
		}
		view.Sources = append(view.Sources, item)
	}
	for _, file := range effective.Files {
		item := FileView{EffectiveFile: file, State: localrepodomain.SyncPending}
		if found, ok := observedFile(status, file.File); ok {
			item.State, item.Message, item.SizeBytes = found.State, found.Message, found.SizeBytes
		}
		if item.State != localrepodomain.SyncReady && observed {
			cause(itemReason(item.State, localrepodomain.ReasonFileFailed), notReady("file "+file.Path, item.State, item.Message))
		}
		view.Files = append(view.Files, item)
	}
	for _, image := range effective.Images {
		item := ImageView{EffectiveImage: image, State: localrepodomain.SyncPending}
		if found, ok := observedImage(status, image.Reference); ok {
			item.State, item.Message, item.SizeBytes = found.State, found.Message, found.SizeBytes
		}
		if item.State != localrepodomain.SyncReady && observed {
			cause(itemReason(item.State, localrepodomain.ReasonImageFailed), notReady("image "+image.Reference, item.State, item.Message))
		}
		view.Images = append(view.Images, item)
	}
	if observed && status.Endpoints == nil {
		cause(localrepodomain.ReasonEndpointsUnknown, "its addresses have not been read yet")
	}
	view.Ready = len(view.NotReady) == 0
	return view
}

// itemReason is the reason of an item that is not ready: failed when its fetch failed, syncing
// while it is pending or syncing.
func itemReason(state localrepodomain.SyncState, failed string) string {
	if state == localrepodomain.SyncFailed {
		return failed
	}
	return localrepodomain.ReasonSyncing
}

// observedSource returns the recorded status of source when it was observed with this definition.
func observedSource(status localrepodomain.Status, source localrepodomain.Source) (localrepodomain.SourceStatus, bool) {
	for _, observed := range status.Sources {
		if observed.Source.Equal(source) {
			return observed.Status, true
		}
	}
	return localrepodomain.SourceStatus{}, false
}

// observedFile returns the recorded status of file when it was observed with this definition.
func observedFile(status localrepodomain.Status, file localrepodomain.File) (localrepodomain.FileStatus, bool) {
	for _, observed := range status.Files {
		if observed.File == file {
			return observed.Status, true
		}
	}
	return localrepodomain.FileStatus{}, false
}

// observedImage returns the recorded status of the image reference when it was observed.
func observedImage(status localrepodomain.Status, reference string) (localrepodomain.ImageStatus, bool) {
	for _, observed := range status.Images {
		if observed.Reference == reference {
			return observed.Status, true
		}
	}
	return localrepodomain.ImageStatus{}, false
}

func notReady(what string, state localrepodomain.SyncState, message string) string {
	if state == localrepodomain.SyncFailed && message != "" {
		return fmt.Sprintf("%s failed: %s", what, message)
	}
	return fmt.Sprintf("%s is %s", what, state)
}

// repositoryIntegration returns the Site's `repository` Integration, enabled or not, or nil when the
// Site has none. A Site has at most one (sitedomain.IntegrationKind.OnePerSite).
func repositoryIntegration(ctx context.Context, integrations sitedomain.IntegrationRepository, siteID string) (*sitedomain.Integration, error) {
	found, err := integrations.List(ctx, sitedomain.IntegrationFilter{SiteID: siteID, Kind: sitedomain.IntegrationKindRepository})
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

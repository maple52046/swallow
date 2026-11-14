package application

import (
	"context"
	"errors"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// fakeTemplates is an in-memory DeploymentTemplates: templates by id, and the generated templates
// EnsureGenerated created.
type fakeTemplates struct {
	templates map[string]*platformdomain.DeploymentTemplateIntent
	names     map[string]bool
	created   []platformdomain.GeneratedTemplate
}

func (f *fakeTemplates) Find(_ context.Context, id string) (*platformdomain.DeploymentTemplateIntent, error) {
	return f.templates[id], nil
}

func (f *fakeTemplates) NameTaken(_ context.Context, integrationID, name, _ string) (bool, error) {
	return f.names[integrationID+"/"+name], nil
}

func (f *fakeTemplates) EnsureGenerated(_ context.Context, generated []platformdomain.GeneratedTemplate, _ platformdomain.MachinePreparation) error {
	for _, g := range generated {
		if f.templates[g.ID] != nil {
			continue
		}
		if f.templates == nil {
			f.templates = map[string]*platformdomain.DeploymentTemplateIntent{}
		}
		f.templates[g.ID] = &platformdomain.DeploymentTemplateIntent{ID: g.ID, IntegrationID: g.IntegrationID, Name: g.Name, Digest: "d-" + g.ID}
		f.created = append(f.created, g)
	}
	return nil
}

// recordingMemberTemplates records the member template records a deployment writes.
type recordingMemberTemplates struct {
	records []platformdomain.MemberTemplateRecord
}

func (r *recordingMemberTemplates) RecordMemberTemplates(_ context.Context, _ string, records []platformdomain.MemberTemplateRecord) error {
	r.records = append(r.records, records...)
	return nil
}

// fakeQueueReaders answers every Platform with a reader that counts queued jobs from jobs, or fails
// with err.
type fakeQueueReaders struct {
	jobs map[string]int
	err  error
}

type fakeQueueReader struct{ readers *fakeQueueReaders }

func (f *fakeQueueReaders) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	if f.err != nil {
		return nil, f.err
	}
	return fakeQueueReader{readers: f}, nil
}

func (r fakeQueueReader) ListMembers(context.Context) ([]platformdomain.Member, error) {
	return nil, nil
}

func (r fakeQueueReader) QueuedJobs(context.Context) (map[string]int, error) {
	if r.readers.jobs == nil {
		return nil, errors.New("no queue")
	}
	return r.readers.jobs, nil
}

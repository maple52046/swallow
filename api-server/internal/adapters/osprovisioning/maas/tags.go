package maas

import (
	"context"
	"net/url"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// This file implements provisioningdomain.MachineTagController against MAAS tags (decision 031).
// MAAS owns tags, so this is the capable half of the capability-first rule: swallow drives MAAS to
// create a manual tag and to assign or unassign it across a batch of machines, and the result is
// mirrored back onto each Server by reconcile. All MAAS vocabulary — the tags endpoints, the
// update_nodes operation, the definition field that marks an automatic tag — stays inside this
// adapter.
//
// MAAS specifics: tags are global objects, not per-machine labels. A tag with a `definition` is
// automatic (MAAS applies it to matching hardware via XPath) and cannot be hand-assigned; a manual
// tag has no definition and is assigned with `POST /tags/{name}/ op=update_nodes` carrying
// repeatable `add` / `remove` system ids, which is batch-native.

// tagPath is the MAAS endpoint for one tag, addressed by its name.
func tagPath(name string) string {
	return "/tags/" + url.PathEscape(name) + "/"
}

// tagJSON is the subset of a MAAS tag swallow reads: its name and whether it is automatic. A tag
// with a non-empty definition is computed by MAAS and cannot be hand-assigned, so it is read-only.
type tagJSON struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// ListTags returns every MAAS tag, flagging a tag Editable when it has no definition — i.e. it is a
// manual tag swallow may assign or unassign rather than one MAAS computes from hardware.
func (p *Provider) ListTags(ctx context.Context) ([]provisioningdomain.MachineTag, error) {
	var out []tagJSON
	if err := p.client.get(ctx, "/tags/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}
	tags := make([]provisioningdomain.MachineTag, 0, len(out))
	for _, tag := range out {
		tags = append(tags, provisioningdomain.MachineTag{
			Name:     tag.Name,
			Editable: tag.Definition == "",
		})
	}
	return tags, nil
}

// EnsureTag creates the named MAAS manual tag unless it already exists.
//
// Existence is checked with a read first rather than by interpreting a create rejection, because
// MAAS phrases the "already exists" validation error as free text that is not safe to pattern
// match. A tag MAAS already holds — manual or automatic — is treated as satisfied; if it turns out
// to be automatic, the following AddTag/RemoveTag is the operation MAAS refuses, and that refusal
// is surfaced rather than a create error being invented here.
func (p *Provider) EnsureTag(ctx context.Context, name string) error {
	exists, err := p.tagExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := p.client.postMultipart(ctx, "/tags/", map[string]string{"name": name}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// AddTag assigns the named tag to every machine in machineIDs in one MAAS update_nodes call.
func (p *Provider) AddTag(ctx context.Context, name string, machineIDs []string) error {
	return p.updateTagNodes(ctx, name, "add", machineIDs)
}

// RemoveTag unassigns the named tag from every machine in machineIDs in one MAAS update_nodes call.
func (p *Provider) RemoveTag(ctx context.Context, name string, machineIDs []string) error {
	return p.updateTagNodes(ctx, name, "remove", machineIDs)
}

// updateTagNodes drives MAAS's batch tag assignment: a single POST /tags/{name}/ op=update_nodes
// with the system ids repeated under add or remove. An empty machine list is a no-op so a diff that
// changed nothing for this group makes no request. An automatic tag rejects update_nodes; MAAS
// answers with a validation error that translateError maps to ProviderErrorRejected, so the refusal
// reaches the operator rather than being dropped.
func (p *Provider) updateTagNodes(ctx context.Context, name, field string, machineIDs []string) error {
	if len(machineIDs) == 0 {
		return nil
	}
	values := url.Values{}
	for _, id := range machineIDs {
		if id != "" {
			values.Add(field, id)
		}
	}
	if len(values) == 0 {
		return nil
	}
	if err := p.client.postOperationValues(ctx, tagPath(name), "update_nodes", values, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// tagExists reports whether MAAS holds a tag with this name. A 404 is a definite "no"; any other
// transport or API failure is propagated so an ensure does not proceed on a guess.
func (p *Provider) tagExists(ctx context.Context, name string) (bool, error) {
	err := p.client.get(ctx, tagPath(name), nil, nil)
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, translateError(err, "")
}

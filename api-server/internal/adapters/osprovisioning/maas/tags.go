package maas

import (
	"context"
	"net/url"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// This file implements provisioningdomain.MachineTagController against MAAS tags (decision 071).
// swallow owns every manual tag in the Server spec, and the Server controller uses this to make
// MAAS match it: create a manual tag and assign or unassign it on machines. The server-tags API uses
// it to delete or rename a tag and to remove an automatic tag from machines. Reconcile reads the
// tags back as the observation, using ListTags to tell automatic tags, which MAAS owns, from manual
// ones. All MAAS vocabulary — the tags endpoints, the update_nodes operation, the definition field,
// the tags MAAS's commissioning maintains — stays inside this adapter.
//
// MAAS specifics: tags are global objects, not per-machine labels. A tag with a `definition` is
// applied by MAAS to the machines its XPath matches, every time MAAS evaluates it. A tag without
// one is assigned with `POST /tags/{name}/ op=update_nodes` carrying repeatable `add` / `remove`
// system ids, which is batch-native; it is renamed with `PUT /tags/{name}/` and deleted with
// `DELETE /tags/{name}/`, which unassigns it from every machine.

// maasMaintainedTags are the tags without a definition that MAAS itself keeps on machines, so
// swallow reports them as automatic (observed only, never drift) yet removable. MAAS's
// commissioning hooks (metadataserver/builtin_scripts/hooks.py) create `virtual` when missing,
// add it to every machine whose system type is not physical, and remove it from physical ones,
// so no manual tag can usefully carry that name. The same hooks maintain switch-hardware tags
// (`switch`, the ASIC tags, vendor and model names such as `mellanox`), but only on machines with a
// switch ASIC; those names stay manual, because on the servers swallow manages they are far more
// likely an admin's own label.
var maasMaintainedTags = map[string]bool{"virtual": true}

// tagPath is the MAAS endpoint for one tag, addressed by its name.
func tagPath(name string) string {
	return "/tags/" + url.PathEscape(name) + "/"
}

// tagJSON is the subset of a MAAS tag swallow reads: its name and its definition, whose presence
// makes the tag one MAAS computes.
type tagJSON struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// ListTags returns every MAAS tag with its kind. A tag with a definition, or one of the tags
// MAAS's commissioning maintains, is automatic (Editable=false). An automatic tag is Removable only
// without a definition: MAAS applies a definition again whenever it evaluates the tag, so taking
// such a tag off a machine would not last, and the only lasting removal is deleting it.
func (p *Provider) ListTags(ctx context.Context) ([]provisioningdomain.MachineTag, error) {
	var out []tagJSON
	if err := p.client.get(ctx, "/tags/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}
	tags := make([]provisioningdomain.MachineTag, 0, len(out))
	for _, tag := range out {
		computed := tag.Definition != ""
		tags = append(tags, provisioningdomain.MachineTag{
			Name:      tag.Name,
			Editable:  !computed && !maasMaintainedTags[tag.Name],
			Removable: !computed,
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
// A tag MAAS no longer holds answers 404, which is satisfied: nothing carries it, and the tag may
// just have been deleted or renamed by the server-tags API while the Server controller converged.
func (p *Provider) RemoveTag(ctx context.Context, name string, machineIDs []string) error {
	err := p.updateTagNodes(ctx, name, "remove", machineIDs)
	if isNotFound(err) {
		return nil
	}
	return err
}

// DeleteTag deletes the named MAAS tag, which unassigns it from every machine; a tag with a
// definition goes with its definition, so MAAS stops applying it. A tag MAAS does not hold (404)
// is satisfied, so a repeated deletion converges. A refusal is surfaced as a rejection.
func (p *Provider) DeleteTag(ctx context.Context, name string) error {
	err := p.client.delete(ctx, tagPath(name))
	if err == nil || isNotFound(err) {
		return nil
	}
	return translateError(err, "")
}

// RenameTag renames the MAAS tag from to to in place (PUT /tags/{from}/ with the new name), so its
// machines keep it without a gap. A missing from is satisfied. When MAAS already holds to — the
// user merges two tags, or the Server controller created to from the new specs before this call —
// MAAS cannot rename onto it, so from is deleted instead and the controller assigns to wherever the
// specs want it. Existence is read first because MAAS phrases a name clash as free text that is not
// safe to pattern match.
func (p *Provider) RenameTag(ctx context.Context, from, to string) error {
	exists, err := p.tagExists(ctx, from)
	if err != nil || !exists {
		return err
	}
	clash, err := p.tagExists(ctx, to)
	if err != nil {
		return err
	}
	if clash {
		return p.DeleteTag(ctx, from)
	}
	if err := p.client.putMultipart(ctx, tagPath(from), map[string]string{"name": to}, nil); err != nil {
		return translateError(err, "")
	}
	return nil
}

// updateTagNodes drives MAAS's batch tag assignment: a single POST /tags/{name}/ op=update_nodes
// with the system ids repeated under add or remove. An empty machine list is a no-op so a diff that
// changed nothing for this group makes no request. A refusal (a validation error, which some MAAS
// versions answer for a tag with a definition) is mapped by translateError to
// ProviderErrorRejected, so it reaches the user rather than being dropped.
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

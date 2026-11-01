package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// platformSpecDoc persists a Platform's spec. Settings and the machine preparation are stored as
// JSON so the module-owned settings keep their JSON types (numbers, nested objects, arrays) without
// the driver's own representations; no query reads inside them.
type platformSpecDoc struct {
	Desired            string              `bson:"desired"`
	Members            []platformMemberDoc `bson:"members"`
	MachinePreparation string              `bson:"machinePreparation,omitempty"`
	Settings           string              `bson:"settings,omitempty"`
	// SealedUserData is the machine preparation's inline cloud-init user data, sealed with the
	// credential key because it may carry secrets; it never appears in MachinePreparation.
	SealedUserData string              `bson:"sealedUserData,omitempty"`
	Removal        *platformRemovalDoc `bson:"removal,omitempty"`
	// userData is the plain user data between toSpecDoc and the repository sealing it.
	userData string
}

type platformRemovalDoc struct {
	ReleaseServers        bool `bson:"releaseServers"`
	Erase                 bool `bson:"erase,omitempty"`
	SecureErase           bool `bson:"secureErase,omitempty"`
	QuickErase            bool `bson:"quickErase,omitempty"`
	UnbindStaticIPs       bool `bson:"unbindStaticIps,omitempty"`
	CancelActiveWorkflows bool `bson:"cancelActiveWorkflows,omitempty"`
}

type platformMemberDoc struct {
	ServerID string   `bson:"serverId"`
	Roles    []string `bson:"roles"`
}

// platformStatusDoc persists what swallow realized and judged for the spec.
type platformStatusDoc struct {
	ObservedGeneration int64                           `bson:"observedGeneration"`
	Conditions         []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
	Applied            *platformSpecDoc                `bson:"applied,omitempty"`
	Realizing          *platformRealizingDoc           `bson:"realizing,omitempty"`
}

type platformRealizingDoc struct {
	Generation int64            `bson:"generation"`
	WorkflowID string           `bson:"workflowId"`
	Spec       *platformSpecDoc `bson:"spec"`
	StartedAt  time.Time        `bson:"startedAt"`
}

// machinePreparationJSON fixes the stored names of the machine preparation independently of the
// domain struct's field names.
type machinePreparationJSON struct {
	Mode           string                  `json:"mode,omitempty"`
	TemplateID     string                  `json:"templateId,omitempty"`
	ImageID        string                  `json:"imageId,omitempty"`
	Ephemeral      *bool                   `json:"ephemeral,omitempty"`
	UserDataMode   string                  `json:"userDataMode,omitempty"`
	UserData       string                  `json:"userData,omitempty"`
	NetworkMode    string                  `json:"networkMode,omitempty"`
	SubnetID       string                  `json:"subnetId,omitempty"`
	DefaultGateway bool                    `json:"defaultGateway,omitempty"`
	Assignments    []machineAssignmentJSON `json:"assignments,omitempty"`
}

type machineAssignmentJSON struct {
	ServerID    string `json:"serverId"`
	InterfaceID string `json:"interfaceId,omitempty"`
	SubnetID    string `json:"subnetId,omitempty"`
	IPAddress   string `json:"ipAddress,omitempty"`
}

func toSpecDoc(spec *platformdomain.PlatformSpec) *platformSpecDoc {
	if spec == nil {
		return nil
	}
	members := make([]platformMemberDoc, len(spec.Members))
	for i, member := range spec.Members {
		members[i] = platformMemberDoc{ServerID: member.ServerID, Roles: nonNilStrings(member.Roles)}
	}
	doc := &platformSpecDoc{Desired: string(spec.Desired), Members: members}
	if len(spec.Settings) > 0 {
		raw, _ := json.Marshal(spec.Settings)
		doc.Settings = string(raw)
	}
	preparation := toMachinePreparationJSON(spec.MachinePreparation)
	preparation.Assignments = toAssignmentJSON(spec.MachinePreparation.Assignments)
	doc.userData, preparation.UserData = preparation.UserData, ""
	if removal := spec.Removal; removal != nil {
		doc.Removal = &platformRemovalDoc{
			ReleaseServers: removal.ReleaseServers, Erase: removal.ReleaseOptions.Erase, SecureErase: removal.ReleaseOptions.SecureErase,
			QuickErase: removal.ReleaseOptions.QuickErase, UnbindStaticIPs: removal.ReleaseOptions.UnbindStaticIPs,
			CancelActiveWorkflows: removal.CancelActiveWorkflows,
		}
	}
	if raw, _ := json.Marshal(preparation); string(raw) != "{}" {
		doc.MachinePreparation = string(raw)
	}
	return doc
}

func (d *platformSpecDoc) toDomain() *platformdomain.PlatformSpec {
	if d == nil {
		return nil
	}
	spec := &platformdomain.PlatformSpec{Desired: platformdomain.PlatformDesired(d.Desired), Members: make([]platformdomain.PlatformMember, len(d.Members))}
	for i, member := range d.Members {
		spec.Members[i] = platformdomain.PlatformMember{ServerID: member.ServerID, Roles: nonNilStrings(member.Roles)}
	}
	if d.Settings != "" {
		_ = json.Unmarshal([]byte(d.Settings), &spec.Settings)
	}
	if removal := d.Removal; removal != nil {
		spec.Removal = &platformdomain.PlatformRemoval{
			ReleaseServers: removal.ReleaseServers, CancelActiveWorkflows: removal.CancelActiveWorkflows,
			ReleaseOptions: platformdomain.ServerReleaseOptions{
				Erase: removal.Erase, SecureErase: removal.SecureErase, QuickErase: removal.QuickErase, UnbindStaticIPs: removal.UnbindStaticIPs,
			},
		}
	}
	if d.MachinePreparation != "" {
		var preparation machinePreparationJSON
		if err := json.Unmarshal([]byte(d.MachinePreparation), &preparation); err == nil {
			spec.MachinePreparation = preparation.toDomain()
		}
	}
	return spec
}

func toMachinePreparationJSON(p platformdomain.MachinePreparation) machinePreparationJSON {
	return machinePreparationJSON{
		Mode: string(p.Mode), TemplateID: p.TemplateID, ImageID: p.ImageID, Ephemeral: p.Ephemeral,
		UserDataMode: p.UserDataMode, UserData: p.UserData, NetworkMode: p.NetworkMode, SubnetID: p.SubnetID,
		DefaultGateway: p.DefaultGateway,
	}
}

func toAssignmentJSON(assignments []platformdomain.MachineNetworkAssignment) []machineAssignmentJSON {
	if len(assignments) == 0 {
		return nil
	}
	out := make([]machineAssignmentJSON, len(assignments))
	for i, a := range assignments {
		out[i] = machineAssignmentJSON{ServerID: a.ServerID, InterfaceID: a.InterfaceID, SubnetID: a.SubnetID, IPAddress: a.IPAddress}
	}
	return out
}

func (j machinePreparationJSON) toDomain() platformdomain.MachinePreparation {
	assignments := make([]platformdomain.MachineNetworkAssignment, len(j.Assignments))
	for i, a := range j.Assignments {
		assignments[i] = platformdomain.MachineNetworkAssignment{ServerID: a.ServerID, InterfaceID: a.InterfaceID, SubnetID: a.SubnetID, IPAddress: a.IPAddress}
	}
	return platformdomain.MachinePreparation{
		Mode: platformdomain.MachinePreparationMode(j.Mode), TemplateID: j.TemplateID, ImageID: j.ImageID, Ephemeral: j.Ephemeral,
		UserDataMode: j.UserDataMode, UserData: j.UserData, NetworkMode: j.NetworkMode, SubnetID: j.SubnetID,
		DefaultGateway: j.DefaultGateway, Assignments: assignments,
	}
}

func toStatusDoc(status platformdomain.PlatformStatus) *platformStatusDoc {
	if status.ObservedGeneration == 0 && len(status.Conditions) == 0 && status.Applied == nil && status.Realizing == nil {
		return nil
	}
	doc := &platformStatusDoc{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         convergencestore.ConditionDocs(status.Conditions),
		Applied:            toSpecDoc(status.Applied),
	}
	if realizing := status.Realizing; realizing != nil {
		doc.Realizing = &platformRealizingDoc{
			Generation: realizing.Generation, WorkflowID: realizing.WorkflowID,
			Spec: toSpecDoc(&realizing.Spec), StartedAt: realizing.StartedAt,
		}
	}
	return doc
}

func (d *platformStatusDoc) toDomain() platformdomain.PlatformStatus {
	if d == nil {
		return platformdomain.PlatformStatus{}
	}
	status := platformdomain.PlatformStatus{
		ObservedGeneration: d.ObservedGeneration,
		Conditions:         convergencestore.ToConditions(d.Conditions),
		Applied:            d.Applied.toDomain(),
	}
	if d.Realizing != nil && d.Realizing.Spec != nil {
		status.Realizing = &platformdomain.PlatformRealizing{
			Generation: d.Realizing.Generation, WorkflowID: d.Realizing.WorkflowID,
			Spec: *d.Realizing.Spec.toDomain(), StartedAt: d.Realizing.StartedAt,
		}
	}
	return status
}

// errUserDataNeedsKey refuses to store inline user data in the clear.
var errUserDataNeedsKey = errors.New("a platform spec with inline user data needs the credential key")

// seal seals the user data of each spec document before it is written.
func (r *MongoPlatformRepo) seal(docs ...*platformSpecDoc) error {
	for _, doc := range docs {
		if doc == nil || doc.userData == "" {
			continue
		}
		if r.sealer == nil {
			return errUserDataNeedsKey
		}
		sealed, err := r.sealer.Seal(doc.userData)
		if err != nil {
			return fmt.Errorf("seal platform spec user data: %w", err)
		}
		doc.SealedUserData = sealed
	}
	return nil
}

// sealStatus seals the spec documents inside a status document.
func (r *MongoPlatformRepo) sealStatus(doc *platformStatusDoc) error {
	if doc == nil {
		return nil
	}
	if doc.Realizing != nil {
		if err := r.seal(doc.Realizing.Spec); err != nil {
			return err
		}
	}
	return r.seal(doc.Applied)
}

// open restores the user data of a read Platform's specs from their documents.
func (r *MongoPlatformRepo) open(doc *platformDoc, platform *platformdomain.Platform) error {
	pairs := []struct {
		doc  *platformSpecDoc
		spec *platformdomain.PlatformSpec
	}{{doc.Spec, platform.Spec}}
	if doc.Status != nil {
		pairs = append(pairs, struct {
			doc  *platformSpecDoc
			spec *platformdomain.PlatformSpec
		}{doc.Status.Applied, platform.Status.Applied})
		if doc.Status.Realizing != nil && platform.Status.Realizing != nil {
			pairs = append(pairs, struct {
				doc  *platformSpecDoc
				spec *platformdomain.PlatformSpec
			}{doc.Status.Realizing.Spec, &platform.Status.Realizing.Spec})
		}
	}
	for _, pair := range pairs {
		if pair.doc == nil || pair.spec == nil || pair.doc.SealedUserData == "" {
			continue
		}
		if r.sealer == nil {
			return errUserDataNeedsKey
		}
		plain, err := r.sealer.Open(pair.doc.SealedUserData)
		if err != nil {
			return fmt.Errorf("open platform spec user data: %w", err)
		}
		pair.spec.MachinePreparation.UserData = plain
	}
	return nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

var _ platformdomain.PlatformSpecStore = (*MongoPlatformRepo)(nil)

// WriteSpec implements platformdomain.PlatformSpecStore with one conditional update on the stored
// generation, so two writers cannot both replace a spec they read at the same generation.
func (r *MongoPlatformRepo) WriteSpec(ctx context.Context, platformID string, expectedGeneration int64, spec platformdomain.PlatformSpec) (int64, error) {
	filter := bson.M{"_id": platformID, "generation": expectedGeneration}
	if expectedGeneration == 0 {
		filter = bson.M{"_id": platformID, "$or": bson.A{bson.M{"generation": 0}, bson.M{"generation": bson.M{"$exists": false}}}}
	}
	next := expectedGeneration + 1
	doc := toSpecDoc(&spec)
	if err := r.seal(doc); err != nil {
		return 0, err
	}
	result, err := r.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"spec": doc, "generation": next, "updatedAt": time.Now().UTC(),
	}})
	if err != nil {
		return 0, err
	}
	if result.MatchedCount == 0 {
		if _, err := r.FindByID(ctx, platformID); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("%w: expected generation %d", platformdomain.ErrGenerationConflict, expectedGeneration)
	}
	return next, nil
}

// WriteStatus implements platformdomain.PlatformSpecStore.
func (r *MongoPlatformRepo) WriteStatus(ctx context.Context, platformID string, status platformdomain.PlatformStatus) error {
	doc := toStatusDoc(status)
	if err := r.sealStatus(doc); err != nil {
		return err
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": platformID}, bson.M{"$set": bson.M{"status": doc}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

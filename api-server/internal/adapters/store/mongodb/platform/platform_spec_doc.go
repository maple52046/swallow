package platform

import (
	"context"
	"encoding/json"
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
	if status.ObservedGeneration == 0 && len(status.Conditions) == 0 && status.Applied == nil {
		return nil
	}
	return &platformStatusDoc{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         convergencestore.ConditionDocs(status.Conditions),
		Applied:            toSpecDoc(status.Applied),
	}
}

func (d *platformStatusDoc) toDomain() platformdomain.PlatformStatus {
	if d == nil {
		return platformdomain.PlatformStatus{}
	}
	return platformdomain.PlatformStatus{
		ObservedGeneration: d.ObservedGeneration,
		Conditions:         convergencestore.ToConditions(d.Conditions),
		Applied:            d.Applied.toDomain(),
	}
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
	result, err := r.col.UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"spec": toSpecDoc(&spec), "generation": next, "updatedAt": time.Now().UTC(),
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
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": platformID}, bson.M{"$set": bson.M{"status": toStatusDoc(status)}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

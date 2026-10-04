package infra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// serverDoc is the MongoDB representation of a server projection.
//
// The health axis is deliberately absent: it is owned by the metrics store and
// resolved at query time, so persisting it would create a copy that is wrong within
// seconds.
type serverDoc struct {
	ID       string      `bson:"_id"`
	Source   sourceDoc   `bson:"source"`
	Hardware hardwareDoc `bson:"hardware"`
	Observed observedDoc `bson:"observed"`

	// GPUs is stored at the top level, not inside observed, because it is written by
	// the inventory sweep on its own cadence. Keeping it out of the observed sub-document
	// lets a reconcile pass rewrite observed wholesale without wiping the sweep's work,
	// the same way membership is left untouched.
	GPUs []gpuDoc `bson:"gpus,omitempty"`

	Deployment   *deploymentDoc   `bson:"deployment,omitempty"`
	Provisioning *provisioningDoc `bson:"provisioning,omitempty"`
	Membership   *membershipDoc   `bson:"membership,omitempty"`

	// DefaultUser is the operator-set Server Default User (decision 045). Top-level and written
	// only by SetDefaultUser, like membership, so a reconcile Upsert never erases it; absent reads
	// as "none set".
	DefaultUser string `bson:"defaultUser,omitempty"`

	// BootMedia and Redfish are the swallow-owned Boot Media setting and Redfish capability
	// (decision 047). Top-level and written only by SetBootMedia / SetRedfishCapability, so a
	// reconcile Upsert never erases them; absent reads as "never set" / "never probed".
	BootMedia *bootMediaDoc `bson:"bootMedia,omitempty"`
	Redfish   *redfishDoc   `bson:"redfish,omitempty"`

	Absent     bool      `bson:"absent"`
	LastSeenAt time.Time `bson:"lastSeenAt"`
	CreatedAt  time.Time `bson:"createdAt"`
	UpdatedAt  time.Time `bson:"updatedAt"`
}

type sourceDoc struct {
	SiteID            string `bson:"siteId"`
	IntegrationID     string `bson:"integrationId"`
	ProviderMachineID string `bson:"providerMachineId"`
}

type hardwareDoc struct {
	SystemUUID   string   `bson:"systemUuid,omitempty"`
	SerialNumber string   `bson:"serialNumber,omitempty"`
	MACAddresses []string `bson:"macAddresses,omitempty"`
}

type observedDoc struct {
	Hostname             string   `bson:"hostname,omitempty"`
	FQDN                 string   `bson:"fqdn,omitempty"`
	Addresses            []string `bson:"addresses,omitempty"`
	Architecture         string   `bson:"architecture,omitempty"`
	CPUCores             int      `bson:"cpuCores,omitempty"`
	CPUModel             string   `bson:"cpuModel,omitempty"`
	MemoryMiB            int64    `bson:"memoryMiB,omitempty"`
	StorageGB            float64  `bson:"storageGB,omitempty"`
	SystemVendor         string   `bson:"systemVendor,omitempty"`
	SystemProduct        string   `bson:"systemProduct,omitempty"`
	ProviderZone         string   `bson:"providerZone,omitempty"`
	ProviderResourcePool string   `bson:"providerResourcePool,omitempty"`
	ProviderPod          string   `bson:"providerPod,omitempty"`
	Tags                 []string `bson:"tags,omitempty"`
}

type gpuDoc struct {
	Vendor string `bson:"vendor"`
	Model  string `bson:"model"`
	Count  int    `bson:"count"`
}

type deploymentDoc struct {
	State        string     `bson:"state"`
	OperationID  string     `bson:"operationId"`
	StepID       string     `bson:"stepId"`
	Attempt      int        `bson:"attempt"`
	Code         string     `bson:"code,omitempty"`
	Stage        string     `bson:"stage,omitempty"`
	StatusReason string     `bson:"statusReason,omitempty"`
	StartedAt    time.Time  `bson:"startedAt"`
	FinishedAt   *time.Time `bson:"finishedAt,omitempty"`
	UpdatedAt    time.Time  `bson:"updatedAt"`
}

type provisioningDoc struct {
	State string `bson:"state"`
	// StateSince is absent on documents written before it existed, which reads as zero
	// (unknown) until the next observation records it.
	StateSince        time.Time `bson:"stateSince,omitempty"`
	ProviderState     string    `bson:"providerState"`
	ErrorDescription  string    `bson:"errorDescription,omitempty"`
	PowerState        string    `bson:"powerState"`
	OSSystem          string    `bson:"osSystem,omitempty"`
	DistroSeries      string    `bson:"distroSeries,omitempty"`
	DeployedImageName string    `bson:"deployedImageName,omitempty"`
	// DeployedImageDefaultUser mirrors the deployed image's effective default login user
	// (decision 039); absent on documents written before it existed, which reads as unknown.
	DeployedImageDefaultUser string    `bson:"deployedImageDefaultUser,omitempty"`
	Ephemeral                bool      `bson:"ephemeral"`
	HWEKernel                string    `bson:"hweKernel,omitempty"`
	Locked                   bool      `bson:"locked"`
	CommissioningStatus      string    `bson:"commissioningStatus,omitempty"`
	TestingStatus            string    `bson:"testingStatus,omitempty"`
	IntegrationID            string    `bson:"integrationId"`
	ObservedAt               time.Time `bson:"observedAt"`
}

// legacyProvisioningStates maps provisioning.state values that earlier releases stored onto
// today's OS Provisioning State. `commissioning` was the MAAS word for `inspecting` until
// decision 048. The state is a mirrored observation that every reconcile pass rewrites, so a
// read-time translation replaces a schema migration; an absent Server keeps its old value
// until it is seen again, which is why the translation must stay until those documents age out.
var legacyProvisioningStates = map[string]string{
	"commissioning": "inspecting",
}

// provisioningStateFromStore returns the current domain value for a stored provisioning.state.
func provisioningStateFromStore(stored string) string {
	if current, ok := legacyProvisioningStates[stored]; ok {
		return current
	}
	return stored
}

// provisioningStateQuery builds the provisioning.state filter for a domain value so that it
// also matches documents still holding a legacy value for the same state.
func provisioningStateQuery(state string) any {
	values := []string{state}
	for legacy, current := range legacyProvisioningStates {
		if current == state {
			values = append(values, legacy)
		}
	}
	if len(values) == 1 {
		return state
	}
	return bson.M{"$in": values}
}

type membershipDoc struct {
	PlatformID string    `bson:"platformId"`
	NodeName   string    `bson:"nodeName"`
	Role       string    `bson:"role,omitempty"`
	State      string    `bson:"state,omitempty"`
	ObservedAt time.Time `bson:"observedAt"`
}

type MongoServerRepo struct {
	col *mongo.Collection
}

// obsoleteIndexes are indexes from the pre-projection server model.
//
// They are dropped by name on startup because two of them are unique on fields the
// projection no longer has: a second server would collide on a null hostname. There is
// no migration framework, and adding one to delete three indexes would be more
// machinery than the problem deserves — but leaving them would break silently on the
// second machine a provisioner reports.
var obsoleteIndexes = []string{
	"hostname_1",
	"ip_1",
	"provisioningSource.provider_1_provisioningSource.machineId_1",
}

func NewMongoServerRepo(db *mongo.Database) (*MongoServerRepo, error) {
	col := db.Collection("servers")

	if err := dropObsoleteIndexes(col); err != nil {
		return nil, err
	}

	indexes := []mongo.IndexModel{
		// The external key. The only uniqueness constraint on a server: hostname and
		// address are observed attributes and legitimately collide across sites.
		{
			Keys: bson.D{
				{Key: "source.siteId", Value: 1},
				{Key: "source.integrationId", Value: 1},
				{Key: "source.providerMachineId", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName("source_key"),
		},
		// Re-enrollment lookup. Sparse so that the many servers with no serial or no
		// system UUID are not all indexed under the empty string.
		{
			Keys:    bson.D{{Key: "hardware.systemUuid", Value: 1}},
			Options: options.Index().SetSparse(true).SetName("hardware_system_uuid"),
		},
		{
			Keys:    bson.D{{Key: "hardware.serialNumber", Value: 1}},
			Options: options.Index().SetSparse(true).SetName("hardware_serial"),
		},
		{
			Keys:    bson.D{{Key: "hardware.macAddresses", Value: 1}},
			Options: options.Index().SetSparse(true).SetName("hardware_macs"),
		},
		// Listing and filtering.
		{
			Keys:    bson.D{{Key: "source.integrationId", Value: 1}, {Key: "lastSeenAt", Value: 1}},
			Options: options.Index().SetName("integration_last_seen"),
		},
		{
			Keys:    bson.D{{Key: "observed.hostname", Value: 1}},
			Options: options.Index().SetName("hostname"),
		},
		{
			Keys:    bson.D{{Key: "membership.platformId", Value: 1}},
			Options: options.Index().SetSparse(true).SetName("membership_platform"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := col.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, err
	}

	return &MongoServerRepo{col: col}, nil
}

// dropObsoleteIndexes removes indexes from the previous model, tolerating their absence
// so that a fresh database is not a special case.
func dropObsoleteIndexes(col *mongo.Collection) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, name := range obsoleteIndexes {
		_, err := col.Indexes().DropOne(ctx, name)
		if err == nil {
			log.Printf("dropped obsolete index %q on servers", name)
			continue
		}
		// Two codes mean there is simply nothing to drop, and both are expected rather
		// than failures: IndexNotFound (27) on a database that has the collection but
		// never carried the old index, and NamespaceNotFound (26) on a fresh database
		// where the servers collection does not exist yet. Anything else is a real
		// failure.
		var cmdErr mongo.CommandError
		if errors.As(err, &cmdErr) && (cmdErr.Code == 26 || cmdErr.Code == 27) {
			continue
		}
		return fmt.Errorf("drop obsolete index %q: %w", name, err)
	}
	return nil
}

func (r *MongoServerRepo) FindByID(ctx context.Context, id string) (*serverdomain.Server, error) {
	var doc serverDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, serverdomain.ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return toServer(&doc), nil
}

func (r *MongoServerRepo) FindBySource(ctx context.Context, source serverdomain.Source) (*serverdomain.Server, error) {
	var doc serverDoc
	err := r.col.FindOne(ctx, bson.M{
		"source.siteId":            source.SiteID,
		"source.integrationId":     source.IntegrationID,
		"source.providerMachineId": source.ProviderMachineID,
	}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, serverdomain.ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return toServer(&doc), nil
}

// FindByHardware matches on any non-empty identifier.
//
// Empty identifiers are excluded before building the query: a clause matching the
// empty string would match every server that lacks that identifier, turning an
// identity lookup into a full scan with arbitrary results.
func (r *MongoServerRepo) FindByHardware(ctx context.Context, hardware serverdomain.Hardware) ([]*serverdomain.Server, error) {
	systemUUID, serialNumber, macs := hardware.Identifiers()

	var clauses bson.A
	if systemUUID != "" {
		clauses = append(clauses, bson.M{"hardware.systemUuid": systemUUID})
	}
	if serialNumber != "" {
		clauses = append(clauses, bson.M{"hardware.serialNumber": serialNumber})
	}
	if len(macs) > 0 {
		clauses = append(clauses, bson.M{"hardware.macAddresses": bson.M{"$in": macs}})
	}
	if len(clauses) == 0 {
		return nil, nil
	}

	cursor, err := r.col.Find(ctx, bson.M{"$or": clauses})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []serverDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	servers := make([]*serverdomain.Server, len(docs))
	for i := range docs {
		servers[i] = toServer(&docs[i])
	}
	return servers, nil
}

func (r *MongoServerRepo) List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	query := bson.M{}

	if filter.SiteID != "" {
		query["source.siteId"] = filter.SiteID
	}
	if filter.IntegrationID != "" {
		query["source.integrationId"] = filter.IntegrationID
	}
	if filter.ProvisioningState != "" {
		query["provisioning.state"] = provisioningStateQuery(filter.ProvisioningState)
	}
	if filter.PlatformID != "" {
		query["membership.platformId"] = filter.PlatformID
	}
	if filter.Tag != "" {
		// Exact membership in the mirrored tag array; MongoDB matches an array field
		// against a scalar by element containment, which is the intended semantics here.
		query["observed.tags"] = filter.Tag
	}
	if !filter.IncludeAbsent {
		query["absent"] = false
	}
	if filter.Keyword != "" {
		regex := bson.M{"$regex": filter.Keyword, "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"observed.hostname": regex},
			bson.M{"observed.fqdn": regex},
			bson.M{"observed.addresses": regex},
		}
	}

	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return serverdomain.ListResult{}, err
	}

	opts := options.Find().
		SetSkip(int64(filter.Offset)).
		SetSort(bson.D{{Key: "observed.hostname", Value: 1}, {Key: "_id", Value: 1}})
	// Limit 0 means unlimited, which the discovery endpoints rely on: a scrape target
	// list or an automation inventory has to be complete.
	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}

	cursor, err := r.col.Find(ctx, query, opts)
	if err != nil {
		return serverdomain.ListResult{}, err
	}
	defer cursor.Close(ctx)

	var docs []serverDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return serverdomain.ListResult{}, err
	}

	servers := make([]*serverdomain.Server, len(docs))
	for i := range docs {
		servers[i] = toServer(&docs[i])
	}

	return serverdomain.ListResult{Servers: servers, Total: int(total)}, nil
}

// Upsert writes the projection and clears the absent flag: a machine the provisioner
// just reported is by definition present.
func (r *MongoServerRepo) Upsert(ctx context.Context, server *serverdomain.Server) error {
	doc := toDoc(server)

	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": server.ID},
		bson.M{
			"$set": bson.M{
				"source":       doc.Source,
				"hardware":     doc.Hardware,
				"observed":     doc.Observed,
				"provisioning": doc.Provisioning,
				"absent":       false,
				"lastSeenAt":   doc.LastSeenAt,
				"updatedAt":    doc.UpdatedAt,
			},
			// Membership is not written here: it is owned by the platform context and
			// would be erased on every reconcile pass if this replaced the document.
			"$setOnInsert": bson.M{"createdAt": doc.CreatedAt},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func (r *MongoServerRepo) MarkAbsent(ctx context.Context, integrationID string, seenBefore time.Time) ([]string, error) {
	filter := bson.M{
		"source.integrationId": integrationID,
		"lastSeenAt":           bson.M{"$lt": seenBefore},
		"absent":               false,
	}
	// Collect the IDs about to flip before the write, so a live consumer can drop exactly
	// the rows that just disappeared. The _id projection keeps this cheap even on a large
	// fleet, matching MarkAbsent's timestamp-sweep intent.
	cursor, err := r.col.Find(ctx, filter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	var idDocs []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &idDocs); err != nil {
		return nil, err
	}
	if len(idDocs) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(idDocs))
	for _, doc := range idDocs {
		ids = append(ids, doc.ID)
	}
	if _, err := r.col.UpdateMany(ctx, filter,
		bson.M{"$set": bson.M{"absent": true, "updatedAt": time.Now().UTC()}}); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *MongoServerRepo) SetMembership(ctx context.Context, id string, membership *serverdomain.MembershipStatus) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if membership == nil {
		update["$unset"] = bson.M{"membership": ""}
	} else {
		update["$set"].(bson.M)["membership"] = membershipDoc{
			PlatformID: membership.PlatformID,
			NodeName:   membership.NodeName,
			Role:       membership.Role,
			State:      membership.State,
			ObservedAt: membership.ObservedAt,
		}
	}

	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

// SetGPUs replaces the GPU inventory, or clears it when gpus is empty.
//
// Written separately from the reconcile Upsert because it is produced by the inventory
// sweep on its own cadence: folding it into Upsert would mean either fetching devices on
// every reconcile pass or having the pass wipe what the sweep found.
func (r *MongoServerRepo) SetGPUs(ctx context.Context, id string, gpus []serverdomain.GPU) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if docs := gpuDocs(gpus); len(docs) > 0 {
		update["$set"].(bson.M)["gpus"] = docs
	} else {
		update["$unset"] = bson.M{"gpus": ""}
	}

	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

// SetDeployment writes the Swallow-owned result independently from provider inventory.
func (r *MongoServerRepo) SetDeployment(ctx context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if deployment == nil {
		update["$unset"] = bson.M{"deployment": ""}
	} else {
		update["$set"].(bson.M)["deployment"] = deploymentDoc{
			State:        string(deployment.State),
			OperationID:  deployment.OperationID,
			StepID:       deployment.StepID,
			Attempt:      deployment.Attempt,
			Code:         deployment.Code,
			Stage:        deployment.Stage,
			StatusReason: deployment.StatusReason,
			StartedAt:    deployment.StartedAt,
			FinishedAt:   deployment.FinishedAt,
			UpdatedAt:    deployment.UpdatedAt,
		}
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

// SetDefaultUser writes the operator-set Server Default User, or unsets the field for "". It
// touches nothing else on the document, so it cannot race the reconcile Upsert's fields.
func (r *MongoServerRepo) SetDefaultUser(ctx context.Context, id string, user string) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if user == "" {
		update["$unset"] = bson.M{"defaultUser": ""}
	} else {
		update["$set"].(bson.M)["defaultUser"] = user
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

// bootMediaDoc stores serverdomain.BootMediaSetting.
type bootMediaDoc struct {
	Enabled       bool       `bson:"enabled"`
	UpdatedAt     time.Time  `bson:"updatedAt"`
	LastAppliedAt *time.Time `bson:"lastAppliedAt,omitempty"`
	LastAppliedBy string     `bson:"lastAppliedBy,omitempty"`
	BootOverride  string     `bson:"bootOverride,omitempty"`
	LastError     string     `bson:"lastError,omitempty"`
	LastErrorAt   *time.Time `bson:"lastErrorAt,omitempty"`
}

// redfishDoc stores serverdomain.RedfishCapability. It holds no credential.
type redfishDoc struct {
	Support           string    `bson:"support"`
	Reason            string    `bson:"reason,omitempty"`
	ServiceRoot       string    `bson:"serviceRoot,omitempty"`
	Vendor            string    `bson:"vendor,omitempty"`
	Product           string    `bson:"product,omitempty"`
	RedfishVersion    string    `bson:"redfishVersion,omitempty"`
	FirmwareVersion   string    `bson:"firmwareVersion,omitempty"`
	SystemID          string    `bson:"systemId,omitempty"`
	VirtualMedia      bool      `bson:"virtualMedia"`
	BootOverrideModes []string  `bson:"bootOverrideModes,omitempty"`
	ProbedAt          time.Time `bson:"probedAt"`
}

// SetBootMedia writes the Server's Boot Media setting, or unsets it for nil. It touches nothing
// else on the document, so it cannot race the reconcile Upsert's fields.
func (r *MongoServerRepo) SetBootMedia(ctx context.Context, id string, setting *serverdomain.BootMediaSetting) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if setting == nil {
		update["$unset"] = bson.M{"bootMedia": ""}
	} else {
		update["$set"].(bson.M)["bootMedia"] = bootMediaDoc{
			Enabled: setting.Enabled, UpdatedAt: setting.UpdatedAt,
			LastAppliedAt: setting.LastAppliedAt, LastAppliedBy: string(setting.LastAppliedBy),
			BootOverride: setting.BootOverride, LastError: setting.LastError, LastErrorAt: setting.LastErrorAt,
		}
	}
	return r.updateExisting(ctx, id, update)
}

// SetRedfishCapability writes the Server's latest Redfish capability probe, or unsets it for nil.
func (r *MongoServerRepo) SetRedfishCapability(ctx context.Context, id string, capability *serverdomain.RedfishCapability) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if capability == nil {
		update["$unset"] = bson.M{"redfish": ""}
	} else {
		update["$set"].(bson.M)["redfish"] = redfishDoc{
			Support: string(capability.Support), Reason: capability.Reason, ServiceRoot: capability.ServiceRoot,
			Vendor: capability.Vendor, Product: capability.Product, RedfishVersion: capability.RedfishVersion,
			FirmwareVersion: capability.FirmwareVersion, SystemID: capability.SystemID,
			VirtualMedia: capability.VirtualMedia, BootOverrideModes: capability.BootOverrideModes,
			ProbedAt: capability.ProbedAt,
		}
	}
	return r.updateExisting(ctx, id, update)
}

// updateExisting applies update to one document and maps "no such document" to ErrServerNotFound.
func (r *MongoServerRepo) updateExisting(ctx context.Context, id string, update bson.M) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

func (r *MongoServerRepo) CountByIntegration(ctx context.Context, integrationID string) (int, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"source.integrationId": integrationID})
	return int(count), err
}

// Delete removes a projection after the application layer has established that its
// provider Machine is absent. The repository deliberately performs no provider I/O.
func (r *MongoServerRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

func toDoc(s *serverdomain.Server) *serverDoc {
	doc := &serverDoc{
		ID: s.ID,
		Source: sourceDoc{
			SiteID:            s.Source.SiteID,
			IntegrationID:     s.Source.IntegrationID,
			ProviderMachineID: s.Source.ProviderMachineID,
		},
		Hardware: hardwareDoc{
			SystemUUID:   s.Hardware.SystemUUID,
			SerialNumber: s.Hardware.SerialNumber,
			MACAddresses: s.Hardware.MACAddresses,
		},
		Observed: observedDoc{
			Hostname:             s.Observed.Hostname,
			FQDN:                 s.Observed.FQDN,
			Addresses:            s.Observed.Addresses,
			Architecture:         s.Observed.Architecture,
			CPUCores:             s.Observed.CPUCores,
			CPUModel:             s.Observed.CPUModel,
			MemoryMiB:            s.Observed.MemoryMiB,
			StorageGB:            s.Observed.StorageGB,
			SystemVendor:         s.Observed.SystemVendor,
			SystemProduct:        s.Observed.SystemProduct,
			ProviderZone:         s.Observed.ProviderZone,
			ProviderResourcePool: s.Observed.ProviderResourcePool,
			ProviderPod:          s.Observed.ProviderPod,
			Tags:                 s.Observed.Tags,
		},
		GPUs:        gpuDocs(s.Observed.GPUs),
		DefaultUser: s.DefaultUser,
		Absent:      s.Absent,
		LastSeenAt:  s.LastSeenAt,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}

	if d := s.Deployment; d != nil {
		doc.Deployment = &deploymentDoc{
			State:        string(d.State),
			OperationID:  d.OperationID,
			StepID:       d.StepID,
			Attempt:      d.Attempt,
			Code:         d.Code,
			Stage:        d.Stage,
			StatusReason: d.StatusReason,
			StartedAt:    d.StartedAt,
			FinishedAt:   d.FinishedAt,
			UpdatedAt:    d.UpdatedAt,
		}
	}

	if p := s.Provisioning; p != nil {
		doc.Provisioning = &provisioningDoc{
			State:                    p.State,
			StateSince:               p.StateSince,
			ProviderState:            p.ProviderState,
			ErrorDescription:         p.ErrorDescription,
			PowerState:               p.PowerState,
			OSSystem:                 p.OSSystem,
			DistroSeries:             p.DistroSeries,
			DeployedImageName:        p.DeployedImageName,
			DeployedImageDefaultUser: p.DeployedImageDefaultUser,
			Ephemeral:                p.Ephemeral,
			HWEKernel:                p.HWEKernel,
			Locked:                   p.Locked,
			CommissioningStatus:      p.CommissioningStatus,
			TestingStatus:            p.TestingStatus,
			IntegrationID:            p.IntegrationID,
			ObservedAt:               p.ObservedAt,
		}
	}

	return doc
}

// gpuDocs converts domain GPUs to their stored form.
func gpuDocs(gpus []serverdomain.GPU) []gpuDoc {
	if len(gpus) == 0 {
		return nil
	}
	docs := make([]gpuDoc, 0, len(gpus))
	for _, gpu := range gpus {
		docs = append(docs, gpuDoc{Vendor: gpu.Vendor, Model: gpu.Model, Count: gpu.Count})
	}
	return docs
}

func toServer(doc *serverDoc) *serverdomain.Server {
	s := &serverdomain.Server{
		ID: doc.ID,
		Source: serverdomain.Source{
			SiteID:            doc.Source.SiteID,
			IntegrationID:     doc.Source.IntegrationID,
			ProviderMachineID: doc.Source.ProviderMachineID,
		},
		Hardware: serverdomain.Hardware{
			SystemUUID:   doc.Hardware.SystemUUID,
			SerialNumber: doc.Hardware.SerialNumber,
			MACAddresses: doc.Hardware.MACAddresses,
		},
		Observed: serverdomain.Observed{
			Hostname:             doc.Observed.Hostname,
			FQDN:                 doc.Observed.FQDN,
			Addresses:            doc.Observed.Addresses,
			Architecture:         doc.Observed.Architecture,
			CPUCores:             doc.Observed.CPUCores,
			CPUModel:             doc.Observed.CPUModel,
			MemoryMiB:            doc.Observed.MemoryMiB,
			StorageGB:            doc.Observed.StorageGB,
			SystemVendor:         doc.Observed.SystemVendor,
			SystemProduct:        doc.Observed.SystemProduct,
			ProviderZone:         doc.Observed.ProviderZone,
			ProviderResourcePool: doc.Observed.ProviderResourcePool,
			ProviderPod:          doc.Observed.ProviderPod,
			Tags:                 doc.Observed.Tags,
		},
		DefaultUser: doc.DefaultUser,
		Absent:      doc.Absent,
		LastSeenAt:  doc.LastSeenAt,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}

	if b := doc.BootMedia; b != nil {
		s.BootMedia = &serverdomain.BootMediaSetting{
			Enabled: b.Enabled, UpdatedAt: b.UpdatedAt,
			LastAppliedAt: b.LastAppliedAt, LastAppliedBy: serverdomain.BootMediaApplier(b.LastAppliedBy),
			BootOverride: b.BootOverride, LastError: b.LastError, LastErrorAt: b.LastErrorAt,
		}
	}
	if r := doc.Redfish; r != nil {
		s.Redfish = &serverdomain.RedfishCapability{
			Support: serverdomain.RedfishSupport(r.Support), Reason: r.Reason, ServiceRoot: r.ServiceRoot,
			Vendor: r.Vendor, Product: r.Product, RedfishVersion: r.RedfishVersion,
			FirmwareVersion: r.FirmwareVersion, SystemID: r.SystemID,
			VirtualMedia: r.VirtualMedia, BootOverrideModes: r.BootOverrideModes, ProbedAt: r.ProbedAt,
		}
	}

	for _, gpu := range doc.GPUs {
		s.Observed.GPUs = append(s.Observed.GPUs, serverdomain.GPU{
			Vendor: gpu.Vendor,
			Model:  gpu.Model,
			Count:  gpu.Count,
		})
	}

	if d := doc.Deployment; d != nil {
		s.Deployment = &serverdomain.DeploymentStatus{
			State:        serverdomain.DeploymentState(d.State),
			OperationID:  d.OperationID,
			StepID:       d.StepID,
			Attempt:      d.Attempt,
			Code:         d.Code,
			Stage:        d.Stage,
			StatusReason: d.StatusReason,
			StartedAt:    d.StartedAt,
			FinishedAt:   d.FinishedAt,
			UpdatedAt:    d.UpdatedAt,
		}
	}

	if p := doc.Provisioning; p != nil {
		s.Provisioning = &serverdomain.ProvisioningStatus{
			State:                    provisioningStateFromStore(p.State),
			StateSince:               p.StateSince,
			ProviderState:            p.ProviderState,
			ErrorDescription:         p.ErrorDescription,
			PowerState:               p.PowerState,
			OSSystem:                 p.OSSystem,
			DistroSeries:             p.DistroSeries,
			DeployedImageName:        p.DeployedImageName,
			DeployedImageDefaultUser: p.DeployedImageDefaultUser,
			Ephemeral:                p.Ephemeral,
			HWEKernel:                p.HWEKernel,
			Locked:                   p.Locked,
			CommissioningStatus:      p.CommissioningStatus,
			TestingStatus:            p.TestingStatus,
			IntegrationID:            p.IntegrationID,
			ObservedAt:               p.ObservedAt,
		}
	}

	if m := doc.Membership; m != nil {
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: m.PlatformID,
			NodeName:   m.NodeName,
			Role:       m.Role,
			State:      m.State,
			ObservedAt: m.ObservedAt,
		}
	}

	return s
}

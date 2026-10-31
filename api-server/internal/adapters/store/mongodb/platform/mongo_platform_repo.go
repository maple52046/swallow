package platform

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

type platformDoc struct {
	ID                 string `bson:"_id"`
	SiteID             string `bson:"siteId"`
	Name               string `bson:"name"`
	Type               string `bson:"type"`
	IntegrationID      string `bson:"integrationId,omitempty"`
	OwnedIntegrationID string `bson:"ownedIntegrationId,omitempty"`
	GPUStackOwner      string `bson:"gpuStackOwner"`
	// ExporterOwner is omitempty so a document written before this field existed
	// decodes as empty and is defaulted to ansible on read.
	ExporterOwner string `bson:"exporterOwner,omitempty"`
	// MonitoringLinks is omitempty so documents written before the field existed — and
	// platforms that never had links — decode as nil, which toPlatform turns into [].
	MonitoringLinks []monitoringLinkDoc `bson:"monitoringLinks,omitempty"`
	// GPUOperator is absent on every Platform Swallow never managed an operator on.
	GPUOperator *gpuOperatorDoc `bson:"gpuOperator,omitempty"`
	// Omnistat is absent on every Platform Swallow never installed Omnistat on.
	Omnistat *omnistatDoc `bson:"omnistat,omitempty"`
	// DeviceMetricsExporter is absent on every Platform Swallow never installed the exporter on.
	DeviceMetricsExporter *deviceMetricsExporterDoc `bson:"deviceMetricsExporter,omitempty"`
	// ContainerRuntime is absent when the Slurm container runtime feature is off.
	ContainerRuntime string `bson:"containerRuntime,omitempty"`
	// GPUDriver is absent when the operator requested no driver options.
	GPUDriver *gpuDriverDoc `bson:"gpuDriver,omitempty"`
	// GPUGresType is absent when the Slurm GRES type is derived from the GPU architecture.
	GPUGresType string `bson:"gpuGresType,omitempty"`
	// OCIContainers and PMIx are absent (false) when the Slurm features are off.
	OCIContainers bool         `bson:"ociContainers,omitempty"`
	PMIx          bool         `bson:"pmix,omitempty"`
	Sync          platformSync `bson:"sync"`
	CreatedAt     time.Time    `bson:"createdAt"`
	UpdatedAt     time.Time    `bson:"updatedAt"`
}

// gpuDriverDoc persists the operator's amdgpu driver intent.
type gpuDriverDoc struct {
	ModuleOptions map[string]string `bson:"moduleOptions,omitempty"`
}

func toGPUDriverDoc(settings platformdomain.GPUDriverSettings) *gpuDriverDoc {
	if settings.Empty() {
		return nil
	}
	return &gpuDriverDoc{ModuleOptions: settings.ModuleOptions}
}

func toGPUDriver(doc *gpuDriverDoc) platformdomain.GPUDriverSettings {
	if doc == nil {
		return platformdomain.GPUDriverSettings{}
	}
	return platformdomain.GPUDriverSettings{ModuleOptions: doc.ModuleOptions}
}

// omnistatDoc persists the Swallow-managed Omnistat record. lastWorkflowId is the fence
// UpdateOmnistatState matches on. installMethod is empty on records written before the native
// method existed, which all used the container method.
type omnistatDoc struct {
	Enabled               bool      `bson:"enabled"`
	Version               string    `bson:"version"`
	InstallMethod         string    `bson:"installMethod,omitempty"`
	ObservabilityServerID string    `bson:"observabilityServerId"`
	State                 string    `bson:"state"`
	LastWorkflowID        string    `bson:"lastWorkflowId"`
	UpdatedAt             time.Time `bson:"updatedAt"`
}

// deviceMetricsExporterDoc persists the Swallow-managed Device Metrics Exporter record.
// lastWorkflowId is the fence UpdateDeviceMetricsExporterState and
// UpdateDeviceMetricsExporterGPUIdentity match on; gpuIdentity is empty until an install reported it.
type deviceMetricsExporterDoc struct {
	Enabled               bool      `bson:"enabled"`
	Version               string    `bson:"version"`
	InstallMethod         string    `bson:"installMethod"`
	ObservabilityServerID string    `bson:"observabilityServerId"`
	State                 string    `bson:"state"`
	GPUIdentity           string    `bson:"gpuIdentity,omitempty"`
	LastWorkflowID        string    `bson:"lastWorkflowId"`
	UpdatedAt             time.Time `bson:"updatedAt"`
}

// gpuOperatorDoc persists the Swallow-managed GPU Operator record. lastWorkflowId is the fence
// UpdateGPUOperatorState matches on.
type gpuOperatorDoc struct {
	Enabled          bool      `bson:"enabled"`
	Version          string    `bson:"version"`
	AllocationMode   string    `bson:"allocationMode"`
	DRADriverVersion string    `bson:"draDriverVersion,omitempty"`
	ResourceNaming   string    `bson:"resourceNaming"`
	NodeLabeller     bool      `bson:"nodeLabeller"`
	MetricsExporter  bool      `bson:"metricsExporter"`
	State            string    `bson:"state"`
	LastWorkflowID   string    `bson:"lastWorkflowId"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

// monitoringLinkDoc persists one operator-registered Monitoring Link.
type monitoringLinkDoc struct {
	Name string `bson:"name"`
	URL  string `bson:"url"`
}

type platformSync struct {
	LastStartedAt   *time.Time `bson:"lastStartedAt,omitempty"`
	LastSucceededAt *time.Time `bson:"lastSucceededAt,omitempty"`
	LastError       string     `bson:"lastError,omitempty"`
	MemberCount     int        `bson:"memberCount"`
	MatchedCount    int        `bson:"matchedCount"`
}

type MongoPlatformRepo struct {
	col *mongo.Collection
}

func NewMongoPlatformRepo(db *mongo.Database) (*MongoPlatformRepo, error) {
	col := db.Collection("platforms")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("site_platform_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoPlatformRepo{col: col}, nil
}

func (r *MongoPlatformRepo) Create(ctx context.Context, platform *platformdomain.Platform) error {
	_, err := r.col.InsertOne(ctx, toDoc(platform))
	if mongo.IsDuplicateKeyError(err) {
		return platformdomain.ErrPlatformNameTaken
	}
	return err
}

func (r *MongoPlatformRepo) FindByID(ctx context.Context, id string) (*platformdomain.Platform, error) {
	var doc platformDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, platformdomain.ErrPlatformNotFound
	}
	if err != nil {
		return nil, err
	}
	return toPlatform(&doc), nil
}

func (r *MongoPlatformRepo) List(ctx context.Context, siteID string) ([]*platformdomain.Platform, error) {
	query := bson.M{}
	if siteID != "" {
		query["siteId"] = siteID
	}

	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []platformDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	platforms := make([]*platformdomain.Platform, len(docs))
	for i := range docs {
		platforms[i] = toPlatform(&docs[i])
	}
	return platforms, nil
}

func (r *MongoPlatformRepo) Update(ctx context.Context, platform *platformdomain.Platform) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": platform.ID},
		bson.M{"$set": bson.M{
			"name":               platform.Name,
			"integrationId":      platform.IntegrationID,
			"ownedIntegrationId": platform.OwnedIntegrationID,
			"gpuStackOwner":      string(platform.GPUStackOwner),
			"exporterOwner":      string(platform.ExporterOwner),
			"monitoringLinks":    toMonitoringLinkDocs(platform.MonitoringLinks),
			"updatedAt":          platform.UpdatedAt,
		}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return platformdomain.ErrPlatformNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

func (r *MongoPlatformRepo) UpdateSyncState(
	ctx context.Context,
	id, integrationID string,
	state platformdomain.SyncState,
) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "integrationId": integrationID},
		bson.M{"$set": bson.M{"sync": platformSync{
			LastStartedAt:   state.LastStartedAt,
			LastSucceededAt: state.LastSucceededAt,
			LastError:       state.LastError,
			MemberCount:     state.MemberCount,
			MatchedCount:    state.MatchedCount,
		}}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

// SetGPUOperator implements platformdomain.GPUOperatorRecorder. It writes the record (or unsets
// it for nil) and the gpuStackOwner policy in one document update so a reader never sees them
// disagree.
func (r *MongoPlatformRepo) SetGPUOperator(
	ctx context.Context,
	platformID string,
	operator *platformdomain.GPUOperator,
	owner platformdomain.GPUStackOwner,
) error {
	update := bson.M{"$set": bson.M{"gpuStackOwner": string(owner), "updatedAt": time.Now().UTC()}}
	if operator == nil {
		update["$unset"] = bson.M{"gpuOperator": ""}
	} else {
		update["$set"].(bson.M)["gpuOperator"] = toGPUOperatorDoc(*operator)
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": platformID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

// UpdateGPUOperatorState implements platformdomain.GPUOperatorRecorder. The filter on
// lastWorkflowId is the fence: a Task of a superseded Workflow matches nothing and changes
// nothing, which is reported as false rather than an error.
func (r *MongoPlatformRepo) UpdateGPUOperatorState(
	ctx context.Context,
	platformID, workflowID string,
	state platformdomain.GPUOperatorState,
	at time.Time,
) (bool, error) {
	if workflowID == "" {
		return false, nil
	}
	set := bson.M{"gpuOperator.state": string(state), "gpuOperator.updatedAt": at}
	if state == platformdomain.GPUOperatorAbsent {
		set["gpuStackOwner"] = string(platformdomain.GPUStackOwnerProvisioning)
	}
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": platformID, "gpuOperator.lastWorkflowId": workflowID},
		bson.M{"$set": set},
	)
	if err != nil {
		return false, err
	}
	return result.MatchedCount > 0, nil
}

// SetOmnistat implements platformdomain.OmnistatRecorder: it writes the record, or unsets it for nil.
func (r *MongoPlatformRepo) SetOmnistat(ctx context.Context, platformID string, omnistat *platformdomain.Omnistat) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if omnistat == nil {
		update["$unset"] = bson.M{"omnistat": ""}
	} else {
		update["$set"].(bson.M)["omnistat"] = toOmnistatDoc(omnistat)
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": platformID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

// UpdateOmnistatState implements platformdomain.OmnistatRecorder. The filter on lastWorkflowId is
// the fence: a Task of a superseded Workflow matches nothing and changes nothing, reported as false.
func (r *MongoPlatformRepo) UpdateOmnistatState(
	ctx context.Context,
	platformID, workflowID string,
	state platformdomain.OmnistatState,
	at time.Time,
) (bool, error) {
	if workflowID == "" {
		return false, nil
	}
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": platformID, "omnistat.lastWorkflowId": workflowID},
		bson.M{"$set": bson.M{"omnistat.state": string(state), "omnistat.updatedAt": at}},
	)
	if err != nil {
		return false, err
	}
	return result.MatchedCount > 0, nil
}

// SetDeviceMetricsExporter implements platformdomain.DeviceMetricsExporterRecorder: it writes the
// record, or unsets it for nil.
func (r *MongoPlatformRepo) SetDeviceMetricsExporter(ctx context.Context, platformID string, exporter *platformdomain.DeviceMetricsExporter) error {
	update := bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}}
	if exporter == nil {
		update["$unset"] = bson.M{"deviceMetricsExporter": ""}
	} else {
		update["$set"].(bson.M)["deviceMetricsExporter"] = toDeviceMetricsExporterDoc(exporter)
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": platformID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

// UpdateDeviceMetricsExporterState implements platformdomain.DeviceMetricsExporterRecorder. The
// filter on lastWorkflowId is the fence: a Task of a superseded Workflow matches nothing and changes
// nothing, reported as false.
func (r *MongoPlatformRepo) UpdateDeviceMetricsExporterState(
	ctx context.Context,
	platformID, workflowID string,
	state platformdomain.DeviceMetricsExporterState,
	at time.Time,
) (bool, error) {
	return r.updateDeviceMetricsExporter(ctx, platformID, workflowID,
		bson.M{"deviceMetricsExporter.state": string(state), "deviceMetricsExporter.updatedAt": at})
}

// UpdateDeviceMetricsExporterGPUIdentity implements platformdomain.DeviceMetricsExporterRecorder,
// under the same fence as the state.
func (r *MongoPlatformRepo) UpdateDeviceMetricsExporterGPUIdentity(
	ctx context.Context,
	platformID, workflowID string,
	identity platformdomain.GPUIdentity,
	at time.Time,
) (bool, error) {
	return r.updateDeviceMetricsExporter(ctx, platformID, workflowID,
		bson.M{"deviceMetricsExporter.gpuIdentity": string(identity), "deviceMetricsExporter.updatedAt": at})
}

func (r *MongoPlatformRepo) updateDeviceMetricsExporter(ctx context.Context, platformID, workflowID string, set bson.M) (bool, error) {
	if workflowID == "" {
		return false, nil
	}
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": platformID, "deviceMetricsExporter.lastWorkflowId": workflowID},
		bson.M{"$set": set},
	)
	if err != nil {
		return false, err
	}
	return result.MatchedCount > 0, nil
}

func toDeviceMetricsExporterDoc(exporter *platformdomain.DeviceMetricsExporter) *deviceMetricsExporterDoc {
	if exporter == nil {
		return nil
	}
	return &deviceMetricsExporterDoc{
		Enabled: exporter.Enabled, Version: exporter.Version, InstallMethod: string(exporter.InstallMethod),
		ObservabilityServerID: exporter.ObservabilityServerID, State: string(exporter.State),
		GPUIdentity: string(exporter.GPUIdentity), LastWorkflowID: exporter.LastWorkflowID,
		UpdatedAt: exporter.UpdatedAt,
	}
}

func toDeviceMetricsExporter(doc *deviceMetricsExporterDoc) *platformdomain.DeviceMetricsExporter {
	if doc == nil {
		return nil
	}
	return &platformdomain.DeviceMetricsExporter{
		Enabled: doc.Enabled, Version: doc.Version,
		InstallMethod:         platformdomain.ObservabilityInstallMethod(doc.InstallMethod),
		ObservabilityServerID: doc.ObservabilityServerID,
		State:                 platformdomain.DeviceMetricsExporterState(doc.State),
		GPUIdentity:           platformdomain.GPUIdentity(doc.GPUIdentity),
		LastWorkflowID:        doc.LastWorkflowID, UpdatedAt: doc.UpdatedAt,
	}
}

func toOmnistatDoc(omnistat *platformdomain.Omnistat) *omnistatDoc {
	if omnistat == nil {
		return nil
	}
	return &omnistatDoc{
		Enabled: omnistat.Enabled, Version: omnistat.Version, InstallMethod: string(omnistat.InstallMethod),
		ObservabilityServerID: omnistat.ObservabilityServerID, State: string(omnistat.State),
		LastWorkflowID: omnistat.LastWorkflowID, UpdatedAt: omnistat.UpdatedAt,
	}
}

func toOmnistat(doc *omnistatDoc) *platformdomain.Omnistat {
	if doc == nil {
		return nil
	}
	method := platformdomain.ObservabilityInstallMethod(doc.InstallMethod)
	if method == "" {
		method = platformdomain.ObservabilityInstallContainer
	}
	return &platformdomain.Omnistat{
		Enabled: doc.Enabled, Version: doc.Version, InstallMethod: method,
		ObservabilityServerID: doc.ObservabilityServerID,
		State:                 platformdomain.OmnistatState(doc.State), LastWorkflowID: doc.LastWorkflowID,
		UpdatedAt: doc.UpdatedAt,
	}
}

func toGPUOperatorDoc(operator platformdomain.GPUOperator) *gpuOperatorDoc {
	settings := operator.Settings
	return &gpuOperatorDoc{
		Enabled: settings.Enabled, Version: settings.Version,
		AllocationMode: string(settings.AllocationMode), DRADriverVersion: settings.DRADriverVersion,
		ResourceNaming: string(settings.ResourceNaming),
		NodeLabeller:   settings.NodeLabeller, MetricsExporter: settings.MetricsExporter,
		State: string(operator.State), LastWorkflowID: operator.LastWorkflowID,
		UpdatedAt: operator.UpdatedAt,
	}
}

func optionalGPUOperatorDoc(operator *platformdomain.GPUOperator) *gpuOperatorDoc {
	if operator == nil {
		return nil
	}
	return toGPUOperatorDoc(*operator)
}

func toGPUOperator(doc *gpuOperatorDoc) *platformdomain.GPUOperator {
	if doc == nil {
		return nil
	}
	return &platformdomain.GPUOperator{
		Settings: platformdomain.GPUOperatorSettings{
			Enabled: doc.Enabled, Version: doc.Version,
			AllocationMode:   platformdomain.GPUAllocationMode(doc.AllocationMode),
			DRADriverVersion: doc.DRADriverVersion,
			ResourceNaming:   platformdomain.GPUResourceNaming(doc.ResourceNaming),
			NodeLabeller:     doc.NodeLabeller, MetricsExporter: doc.MetricsExporter,
		},
		State: platformdomain.GPUOperatorState(doc.State), LastWorkflowID: doc.LastWorkflowID,
		UpdatedAt: doc.UpdatedAt,
	}
}

func (r *MongoPlatformRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

func toDoc(platform *platformdomain.Platform) *platformDoc {
	return &platformDoc{
		ID:                    platform.ID,
		SiteID:                platform.SiteID,
		Name:                  platform.Name,
		Type:                  string(platform.Type),
		IntegrationID:         platform.IntegrationID,
		OwnedIntegrationID:    platform.OwnedIntegrationID,
		GPUStackOwner:         string(platform.GPUStackOwner),
		ExporterOwner:         string(platform.ExporterOwner),
		MonitoringLinks:       toMonitoringLinkDocs(platform.MonitoringLinks),
		GPUOperator:           optionalGPUOperatorDoc(platform.GPUOperator),
		Omnistat:              toOmnistatDoc(platform.Omnistat),
		DeviceMetricsExporter: toDeviceMetricsExporterDoc(platform.DeviceMetricsExporter),
		ContainerRuntime:      string(platform.ContainerRuntime),
		GPUDriver:             toGPUDriverDoc(platform.GPUDriver),
		GPUGresType:           platform.GPUGresType,
		OCIContainers:         platform.OCIContainers,
		PMIx:                  platform.PMIx,
		Sync: platformSync{
			LastStartedAt:   platform.Sync.LastStartedAt,
			LastSucceededAt: platform.Sync.LastSucceededAt,
			LastError:       platform.Sync.LastError,
			MemberCount:     platform.Sync.MemberCount,
			MatchedCount:    platform.Sync.MatchedCount,
		},
		CreatedAt: platform.CreatedAt,
		UpdatedAt: platform.UpdatedAt,
	}
}

func toPlatform(doc *platformDoc) *platformdomain.Platform {
	// A platform stored before exporterOwner existed decodes as empty; treat that as the
	// default ansible owner rather than an invalid empty value.
	exporterOwner := platformdomain.ExporterOwner(doc.ExporterOwner)
	if exporterOwner == "" {
		exporterOwner = platformdomain.ExporterOwnerAnsible
	}
	return &platformdomain.Platform{
		ID:                    doc.ID,
		SiteID:                doc.SiteID,
		Name:                  doc.Name,
		Type:                  platformdomain.PlatformType(doc.Type),
		IntegrationID:         doc.IntegrationID,
		OwnedIntegrationID:    doc.OwnedIntegrationID,
		GPUStackOwner:         platformdomain.GPUStackOwner(doc.GPUStackOwner),
		ExporterOwner:         exporterOwner,
		MonitoringLinks:       toMonitoringLinks(doc.MonitoringLinks),
		GPUOperator:           toGPUOperator(doc.GPUOperator),
		Omnistat:              toOmnistat(doc.Omnistat),
		DeviceMetricsExporter: toDeviceMetricsExporter(doc.DeviceMetricsExporter),
		ContainerRuntime:      platformdomain.ContainerRuntime(doc.ContainerRuntime),
		GPUDriver:             toGPUDriver(doc.GPUDriver),
		GPUGresType:           doc.GPUGresType,
		OCIContainers:         doc.OCIContainers,
		PMIx:                  doc.PMIx,
		Sync: platformdomain.SyncState{
			LastStartedAt:   doc.Sync.LastStartedAt,
			LastSucceededAt: doc.Sync.LastSucceededAt,
			LastError:       doc.Sync.LastError,
			MemberCount:     doc.Sync.MemberCount,
			MatchedCount:    doc.Sync.MatchedCount,
		},
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
}

// toMonitoringLinkDocs always returns a non-nil slice so an Update that clears the links stores
// an empty array rather than null; the omitempty tag then keeps inserts free of an empty field.
func toMonitoringLinkDocs(links []platformdomain.MonitoringLink) []monitoringLinkDoc {
	docs := make([]monitoringLinkDoc, len(links))
	for i, link := range links {
		docs[i] = monitoringLinkDoc{Name: link.Name, URL: link.URL}
	}
	return docs
}

// toMonitoringLinks maps stored links, reading a missing field (pre-2026-10-12 documents) as [].
func toMonitoringLinks(docs []monitoringLinkDoc) []platformdomain.MonitoringLink {
	links := make([]platformdomain.MonitoringLink, len(docs))
	for i, doc := range docs {
		links[i] = platformdomain.MonitoringLink{Name: doc.Name, URL: doc.URL}
	}
	return links
}

package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// serverDoc is the MongoDB BSON representation of a Server.
// Inventory and agent sub-documents are optional and updated independently
// via $set so that unrelated fields are never overwritten.
type serverDoc struct {
	ID        string          `bson:"_id"`
	Hostname  string          `bson:"hostname"`
	IP        string          `bson:"ip"`
	Status    string          `bson:"status"`
	Inventory *inventoryDoc   `bson:"inventory,omitempty"`
	Agent     *agentDoc       `bson:"agent,omitempty"`
	CreatedAt time.Time       `bson:"createdAt"`
	UpdatedAt time.Time       `bson:"updatedAt"`
}

type inventoryDoc struct {
	OS        osDoc        `bson:"os"`
	CPU       cpuDoc       `bson:"cpu"`
	Memory    memoryDoc    `bson:"memory"`
	GPUs      []gpuDoc     `bson:"gpus"`
	Network   networkDoc   `bson:"network"`
	IPMI      ipmiDoc      `bson:"ipmi"`
	UpdatedAt time.Time    `bson:"updatedAt"`
}

type osDoc struct {
	Type          string `bson:"type"`
	Distribution  string `bson:"distribution"`
	Version       string `bson:"version"`
	KernelVersion string `bson:"kernelVersion"`
	Architecture  string `bson:"architecture"`
}

type cpuDoc struct {
	Model   string `bson:"model"`
	Cores   int32  `bson:"cores"`
	Threads int32  `bson:"threads"`
}

type memoryDoc struct {
	TotalKB int64 `bson:"totalKB"`
}

type gpuDoc struct {
	Vendor string `bson:"vendor"`
	Model  string `bson:"model"`
	Index  int32  `bson:"index"`
}

type networkDoc struct {
	Hostname  string   `bson:"hostname"`
	PrimaryIP string   `bson:"primaryIP"`
	AllIPs    []string `bson:"allIPs"`
}

type ipmiDoc struct {
	Available bool   `bson:"available"`
	BMCIP     string `bson:"bmcIP"`
	Source    string `bson:"source"`
	Status    string `bson:"status"`
}

type agentDoc struct {
	Status       string    `bson:"status"`
	LastSeenAt   time.Time `bson:"lastSeenAt"`
	AgentVersion string    `bson:"agentVersion"`
}

type MongoServerRepo struct {
	col *mongo.Collection
}

func NewMongoServerRepo(db *mongo.Database) (*MongoServerRepo, error) {
	col := db.Collection("servers")

	indexModels := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "hostname", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "ip", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := col.Indexes().CreateMany(ctx, indexModels); err != nil {
		return nil, err
	}

	return &MongoServerRepo{col: col}, nil
}

func (r *MongoServerRepo) Create(ctx context.Context, server *serverdomain.Server) error {
	doc := serverDoc{
		ID:        server.ID,
		Hostname:  server.Hostname,
		IP:        server.IP,
		Status:    string(server.Status),
		CreatedAt: server.CreatedAt,
		UpdatedAt: server.UpdatedAt,
	}
	_, err := r.col.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return serverdomain.ErrHostnameTaken
	}
	return err
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

func (r *MongoServerRepo) List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	query := bson.M{}

	if filter.Status != "" {
		query["status"] = string(filter.Status)
	}
	if filter.Keyword != "" {
		regex := bson.M{"$regex": filter.Keyword, "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"hostname": regex},
			bson.M{"ip": regex},
		}
	}

	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return serverdomain.ListResult{}, err
	}

	opts := options.Find().
		SetSkip(int64(filter.Offset)).
		SetLimit(int64(filter.Limit)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

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
	for i, d := range docs {
		servers[i] = toServer(&d)
	}

	return serverdomain.ListResult{Servers: servers, Total: int(total)}, nil
}

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

func (r *MongoServerRepo) ExistsByHostname(ctx context.Context, hostname string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"hostname": hostname})
	return count > 0, err
}

func (r *MongoServerRepo) ExistsByIP(ctx context.Context, ip string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"ip": ip})
	return count > 0, err
}

// UpdateInventory replaces the inventory sub-document for the given node.
// Only inventory fields are modified; existing node metadata is untouched.
// Returns ErrServerNotFound if the node does not exist.
func (r *MongoServerRepo) UpdateInventory(ctx context.Context, id string, inv serverdomain.Inventory) error {
	doc := inventoryDoc{
		OS: osDoc{
			Type:          inv.OS.Type,
			Distribution:  inv.OS.Distribution,
			Version:       inv.OS.Version,
			KernelVersion: inv.OS.KernelVersion,
			Architecture:  inv.OS.Architecture,
		},
		CPU: cpuDoc{
			Model:   inv.CPU.Model,
			Cores:   inv.CPU.Cores,
			Threads: inv.CPU.Threads,
		},
		Memory: memoryDoc{TotalKB: inv.Memory.TotalKB},
		Network: networkDoc{
			Hostname:  inv.Network.Hostname,
			PrimaryIP: inv.Network.PrimaryIP,
			AllIPs:    inv.Network.AllIPs,
		},
		IPMI: ipmiDoc{
			Available: inv.IPMI.Available,
			BMCIP:     inv.IPMI.BMCIP,
			Source:    inv.IPMI.Source,
			Status:    inv.IPMI.Status,
		},
		UpdatedAt: inv.UpdatedAt,
	}
	for _, g := range inv.GPUs {
		doc.GPUs = append(doc.GPUs, gpuDoc{Vendor: g.Vendor, Model: g.Model, Index: g.Index})
	}

	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"inventory": doc}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

// UpdateAgentInfo replaces the agent sub-document for the given node.
// Only agent connectivity fields are modified; hardware inventory is untouched.
// Returns ErrServerNotFound if the node does not exist.
func (r *MongoServerRepo) UpdateAgentInfo(ctx context.Context, id string, info serverdomain.AgentInfo) error {
	doc := agentDoc{
		Status:       string(info.Status),
		LastSeenAt:   info.LastSeenAt,
		AgentVersion: info.AgentVersion,
	}
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"agent": doc}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

func toServer(doc *serverDoc) *serverdomain.Server {
	s := &serverdomain.Server{
		ID:        doc.ID,
		Hostname:  doc.Hostname,
		IP:        doc.IP,
		Status:    serverdomain.ServerStatus(doc.Status),
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
	if doc.Inventory != nil {
		inv := &serverdomain.Inventory{
			OS: serverdomain.OSInfo{
				Type:          doc.Inventory.OS.Type,
				Distribution:  doc.Inventory.OS.Distribution,
				Version:       doc.Inventory.OS.Version,
				KernelVersion: doc.Inventory.OS.KernelVersion,
				Architecture:  doc.Inventory.OS.Architecture,
			},
			CPU: serverdomain.CPUInfo{
				Model:   doc.Inventory.CPU.Model,
				Cores:   doc.Inventory.CPU.Cores,
				Threads: doc.Inventory.CPU.Threads,
			},
			Memory: serverdomain.MemoryInfo{TotalKB: doc.Inventory.Memory.TotalKB},
			Network: serverdomain.NetworkInfo{
				Hostname:  doc.Inventory.Network.Hostname,
				PrimaryIP: doc.Inventory.Network.PrimaryIP,
				AllIPs:    doc.Inventory.Network.AllIPs,
			},
			IPMI: serverdomain.IPMIInfo{
				Available: doc.Inventory.IPMI.Available,
				BMCIP:     doc.Inventory.IPMI.BMCIP,
				Source:    doc.Inventory.IPMI.Source,
				Status:    doc.Inventory.IPMI.Status,
			},
			UpdatedAt: doc.Inventory.UpdatedAt,
		}
		for _, g := range doc.Inventory.GPUs {
			inv.GPUs = append(inv.GPUs, serverdomain.GPUInfo{Vendor: g.Vendor, Model: g.Model, Index: g.Index})
		}
		s.Inventory = inv
	}
	if doc.Agent != nil {
		s.Agent = &serverdomain.AgentInfo{
			Status:       serverdomain.AgentStatus(doc.Agent.Status),
			LastSeenAt:   doc.Agent.LastSeenAt,
			AgentVersion: doc.Agent.AgentVersion,
		}
	}
	return s
}

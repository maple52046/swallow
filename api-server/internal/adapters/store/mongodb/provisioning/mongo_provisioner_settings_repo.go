package provisioning

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// httpProxyConfigDoc is the stored shape of an HTTP Proxy configuration.
type httpProxyConfigDoc struct {
	Mode string `bson:"mode"`
	URL  string `bson:"url,omitempty"`
}

// httpProxyDoc is one Integration's HTTP Proxy setting, keyed by the Integration id.
type httpProxyDoc struct {
	IntegrationID string             `bson:"_id"`
	Generation    int64              `bson:"generation"`
	Spec          httpProxyConfigDoc `bson:"spec"`
	Status        struct {
		ObservedGeneration int64                           `bson:"observedGeneration"`
		Conditions         []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
		Observed           *httpProxyConfigDoc             `bson:"observed,omitempty"`
		ObservedAt         *time.Time                      `bson:"observedAt,omitempty"`
	} `bson:"status"`
}

// MongoHTTPProxyRepo stores HTTP Proxy settings in `provisioner_http_proxies`.
type MongoHTTPProxyRepo struct {
	col *mongo.Collection
}

var _ provisioningdomain.HTTPProxyStore = (*MongoHTTPProxyRepo)(nil)

// NewMongoHTTPProxyRepo returns the repository; the Integration id is the document id, so no
// index is needed.
func NewMongoHTTPProxyRepo(db *mongo.Database) *MongoHTTPProxyRepo {
	return &MongoHTTPProxyRepo{col: db.Collection("provisioner_http_proxies")}
}

func (r *MongoHTTPProxyRepo) Get(ctx context.Context, integrationID string) (*provisioningdomain.HTTPProxySetting, error) {
	var doc httpProxyDoc
	err := r.col.FindOne(ctx, bson.M{"_id": integrationID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, provisioningdomain.ErrSettingNotFound
	}
	if err != nil {
		return nil, err
	}
	return toHTTPProxySetting(&doc), nil
}

func (r *MongoHTTPProxyRepo) List(ctx context.Context) ([]*provisioningdomain.HTTPProxySetting, error) {
	cursor, err := r.col.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var docs []httpProxyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*provisioningdomain.HTTPProxySetting, len(docs))
	for i := range docs {
		out[i] = toHTTPProxySetting(&docs[i])
	}
	return out, nil
}

func (r *MongoHTTPProxyRepo) Adopt(ctx context.Context, setting *provisioningdomain.HTTPProxySetting) (bool, error) {
	_, err := r.col.InsertOne(ctx, fromHTTPProxySetting(setting))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *MongoHTTPProxyRepo) PutSpec(ctx context.Context, integrationID string, spec provisioningdomain.HTTPProxyConfiguration) (*provisioningdomain.HTTPProxySetting, error) {
	var doc httpProxyDoc
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": integrationID},
		bson.M{
			"$set": bson.M{"spec": httpProxyConfigDoc{Mode: string(spec.Mode), URL: spec.URL}},
			"$inc": bson.M{"generation": 1},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return toHTTPProxySetting(&doc), nil
}

func (r *MongoHTTPProxyRepo) SetStatus(ctx context.Context, integrationID string, status provisioningdomain.HTTPProxyStatus) error {
	doc := fromHTTPProxySetting(&provisioningdomain.HTTPProxySetting{Status: status})
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": integrationID}, bson.M{"$set": bson.M{"status": doc.Status}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrSettingNotFound
	}
	return nil
}

func (r *MongoHTTPProxyRepo) Delete(ctx context.Context, integrationID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": integrationID})
	return err
}

func fromHTTPProxySetting(s *provisioningdomain.HTTPProxySetting) httpProxyDoc {
	doc := httpProxyDoc{
		IntegrationID: s.IntegrationID, Generation: s.Generation,
		Spec: httpProxyConfigDoc{Mode: string(s.Spec.Mode), URL: s.Spec.URL},
	}
	doc.Status.ObservedGeneration = s.Status.ObservedGeneration
	doc.Status.Conditions = convergencestore.ConditionDocs(s.Status.Conditions)
	if s.Status.Observed != nil {
		doc.Status.Observed = &httpProxyConfigDoc{Mode: string(s.Status.Observed.Mode), URL: s.Status.Observed.URL}
	}
	if s.Status.ObservedAt != nil {
		at := s.Status.ObservedAt.UTC()
		doc.Status.ObservedAt = &at
	}
	return doc
}

func toHTTPProxySetting(doc *httpProxyDoc) *provisioningdomain.HTTPProxySetting {
	s := &provisioningdomain.HTTPProxySetting{
		IntegrationID: doc.IntegrationID, Generation: doc.Generation,
		Spec: provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyMode(doc.Spec.Mode), URL: doc.Spec.URL},
		Status: provisioningdomain.HTTPProxyStatus{
			ObservedGeneration: doc.Status.ObservedGeneration,
			Conditions:         convergencestore.ToConditions(doc.Status.Conditions),
			ObservedAt:         doc.Status.ObservedAt,
		},
	}
	if doc.Status.Observed != nil {
		s.Status.Observed = &provisioningdomain.HTTPProxyConfiguration{
			Mode: provisioningdomain.HTTPProxyMode(doc.Status.Observed.Mode), URL: doc.Status.Observed.URL,
		}
	}
	return s
}

// subnetDNSDoc is one subnet's DNS setting, keyed by "<integrationId>/<subnetId>".
type subnetDNSDoc struct {
	ID            string   `bson:"_id"`
	IntegrationID string   `bson:"integrationId"`
	SubnetID      string   `bson:"subnetId"`
	Generation    int64    `bson:"generation"`
	Servers       []string `bson:"servers"`
	Status        struct {
		ObservedGeneration int64                           `bson:"observedGeneration"`
		Conditions         []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
		Observed           []string                        `bson:"observed"`
		ObservedAt         *time.Time                      `bson:"observedAt,omitempty"`
	} `bson:"status"`
}

// SubnetDNSKey is the document id of a subnet's setting. The change feed reports it as the key,
// so controllers address a setting by it.
func SubnetDNSKey(integrationID, subnetID string) string {
	return integrationID + "/" + subnetID
}

// MongoSubnetDNSRepo stores Subnet DNS settings in `provisioner_subnet_dns`.
type MongoSubnetDNSRepo struct {
	col *mongo.Collection
}

var _ provisioningdomain.SubnetDNSStore = (*MongoSubnetDNSRepo)(nil)

// NewMongoSubnetDNSRepo creates the per-Integration index and returns the repository.
func NewMongoSubnetDNSRepo(db *mongo.Database) (*MongoSubnetDNSRepo, error) {
	col := db.Collection("provisioner_subnet_dns")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "integrationId", Value: 1}}, Options: options.Index().SetName("integration"),
	}); err != nil {
		return nil, err
	}
	return &MongoSubnetDNSRepo{col: col}, nil
}

func (r *MongoSubnetDNSRepo) Get(ctx context.Context, integrationID, subnetID string) (*provisioningdomain.SubnetDNSSetting, error) {
	var doc subnetDNSDoc
	err := r.col.FindOne(ctx, bson.M{"_id": SubnetDNSKey(integrationID, subnetID)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, provisioningdomain.ErrSettingNotFound
	}
	if err != nil {
		return nil, err
	}
	return toSubnetDNSSetting(&doc), nil
}

func (r *MongoSubnetDNSRepo) ListByIntegration(ctx context.Context, integrationID string) ([]*provisioningdomain.SubnetDNSSetting, error) {
	return r.find(ctx, bson.M{"integrationId": integrationID})
}

func (r *MongoSubnetDNSRepo) List(ctx context.Context) ([]*provisioningdomain.SubnetDNSSetting, error) {
	return r.find(ctx, bson.M{})
}

func (r *MongoSubnetDNSRepo) find(ctx context.Context, filter bson.M) ([]*provisioningdomain.SubnetDNSSetting, error) {
	cursor, err := r.col.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var docs []subnetDNSDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*provisioningdomain.SubnetDNSSetting, len(docs))
	for i := range docs {
		out[i] = toSubnetDNSSetting(&docs[i])
	}
	return out, nil
}

func (r *MongoSubnetDNSRepo) Adopt(ctx context.Context, setting *provisioningdomain.SubnetDNSSetting) (bool, error) {
	_, err := r.col.InsertOne(ctx, fromSubnetDNSSetting(setting))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *MongoSubnetDNSRepo) PutSpec(ctx context.Context, integrationID, subnetID string, servers []string) (*provisioningdomain.SubnetDNSSetting, error) {
	if servers == nil {
		servers = []string{}
	}
	var doc subnetDNSDoc
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": SubnetDNSKey(integrationID, subnetID)},
		bson.M{
			"$set":         bson.M{"servers": servers},
			"$setOnInsert": bson.M{"integrationId": integrationID, "subnetId": subnetID},
			"$inc":         bson.M{"generation": 1},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return toSubnetDNSSetting(&doc), nil
}

func (r *MongoSubnetDNSRepo) SetStatus(ctx context.Context, integrationID, subnetID string, status provisioningdomain.SubnetDNSStatus) error {
	doc := fromSubnetDNSSetting(&provisioningdomain.SubnetDNSSetting{Status: status})
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": SubnetDNSKey(integrationID, subnetID)}, bson.M{"$set": bson.M{"status": doc.Status}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrSettingNotFound
	}
	return nil
}

func (r *MongoSubnetDNSRepo) Delete(ctx context.Context, integrationID, subnetID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": SubnetDNSKey(integrationID, subnetID)})
	return err
}

func fromSubnetDNSSetting(s *provisioningdomain.SubnetDNSSetting) subnetDNSDoc {
	doc := subnetDNSDoc{
		ID: SubnetDNSKey(s.IntegrationID, s.SubnetID), IntegrationID: s.IntegrationID, SubnetID: s.SubnetID,
		Generation: s.Generation, Servers: nonNil(s.Servers),
	}
	doc.Status.ObservedGeneration = s.Status.ObservedGeneration
	doc.Status.Conditions = convergencestore.ConditionDocs(s.Status.Conditions)
	doc.Status.Observed = nonNil(s.Status.Observed)
	if s.Status.ObservedAt != nil {
		at := s.Status.ObservedAt.UTC()
		doc.Status.ObservedAt = &at
	}
	return doc
}

func toSubnetDNSSetting(doc *subnetDNSDoc) *provisioningdomain.SubnetDNSSetting {
	return &provisioningdomain.SubnetDNSSetting{
		IntegrationID: doc.IntegrationID, SubnetID: doc.SubnetID, Generation: doc.Generation,
		Servers: emptyToNil(doc.Servers),
		Status: provisioningdomain.SubnetDNSStatus{
			ObservedGeneration: doc.Status.ObservedGeneration,
			Conditions:         convergencestore.ToConditions(doc.Status.Conditions),
			Observed:           emptyToNil(doc.Status.Observed),
			ObservedAt:         doc.Status.ObservedAt,
		},
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func emptyToNil(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

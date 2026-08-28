package infra

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

type deploymentTemplateDoc struct {
	ID             string    `bson:"_id"`
	IntegrationID  string    `bson:"integrationId"`
	NormalizedName string    `bson:"normalizedName"`
	Name           string    `bson:"name"`
	Description    string    `bson:"description,omitempty"`
	ImageID        string    `bson:"imageId"`
	Ephemeral      bool      `bson:"ephemeral"`
	SealedUserData string    `bson:"sealedUserData,omitempty"`
	CreatedAt      time.Time `bson:"createdAt"`
	UpdatedAt      time.Time `bson:"updatedAt"`
}

// MongoDeploymentTemplateRepo stores template intent and sealed cloud-init.
type MongoDeploymentTemplateRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

// NewMongoDeploymentTemplateRepo creates indexes and returns a template repository.
func NewMongoDeploymentTemplateRepo(
	db *mongo.Database,
	sealer *secret.Sealer,
) (*MongoDeploymentTemplateRepo, error) {
	col := db.Collection("deployment_templates")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "integrationId", Value: 1},
				{Key: "normalizedName", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName("integration_normalized_name"),
		},
		{
			Keys:    bson.D{{Key: "integrationId", Value: 1}, {Key: "name", Value: 1}},
			Options: options.Index().SetName("integration_name"),
		},
	})
	if err != nil {
		return nil, err
	}
	return &MongoDeploymentTemplateRepo{col: col, sealer: sealer}, nil
}

// Create seals optional user data before inserting the template.
func (r *MongoDeploymentTemplateRepo) Create(
	ctx context.Context,
	template *provisioningdomain.DeploymentTemplate,
	userData string,
) error {
	doc, err := newDeploymentTemplateDoc(template, userData, r.sealer)
	if err != nil {
		return err
	}
	_, err = r.col.InsertOne(ctx, doc)
	return mapTemplateWriteError(err)
}

func newDeploymentTemplateDoc(
	template *provisioningdomain.DeploymentTemplate,
	userData string,
	sealer *secret.Sealer,
) (deploymentTemplateDoc, error) {
	doc := deploymentTemplateDoc{
		ID:             template.ID,
		IntegrationID:  template.IntegrationID,
		NormalizedName: normalizeTemplateName(template.Name),
		Name:           template.Name,
		Description:    template.Description,
		ImageID:        template.ImageID,
		Ephemeral:      template.Ephemeral,
		CreatedAt:      template.CreatedAt,
		UpdatedAt:      template.UpdatedAt,
	}
	if userData == "" {
		return doc, nil
	}
	sealed, err := sealer.Seal(userData)
	if err != nil {
		return deploymentTemplateDoc{}, err
	}
	doc.SealedUserData = sealed
	return doc, nil
}

// FindByID returns template metadata without decrypting its user data.
func (r *MongoDeploymentTemplateRepo) FindByID(
	ctx context.Context,
	id string,
) (*provisioningdomain.DeploymentTemplate, error) {
	var doc deploymentTemplateDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, provisioningdomain.ErrDeploymentTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDeploymentTemplate(&doc), nil
}

// List returns template metadata sorted for operator scanning.
func (r *MongoDeploymentTemplateRepo) List(
	ctx context.Context,
	filter provisioningdomain.DeploymentTemplateFilter,
) ([]*provisioningdomain.DeploymentTemplate, error) {
	query := bson.M{}
	if filter.IntegrationID != "" {
		query["integrationId"] = filter.IntegrationID
	}
	if len(filter.IntegrationIDs) > 0 {
		query["integrationId"] = bson.M{"$in": filter.IntegrationIDs}
	}
	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{
		{Key: "integrationId", Value: 1},
		{Key: "normalizedName", Value: 1},
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []deploymentTemplateDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	items := make([]*provisioningdomain.DeploymentTemplate, len(docs))
	for i := range docs {
		items[i] = toDeploymentTemplate(&docs[i])
	}
	return items, nil
}

// Update changes non-secret template intent while preserving its integration and secret.
func (r *MongoDeploymentTemplateRepo) Update(
	ctx context.Context,
	template *provisioningdomain.DeploymentTemplate,
) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": template.ID}, bson.M{"$set": bson.M{
		"normalizedName": normalizeTemplateName(template.Name),
		"name":           template.Name,
		"description":    template.Description,
		"imageId":        template.ImageID,
		"ephemeral":      template.Ephemeral,
		"updatedAt":      template.UpdatedAt,
	}})
	if err = mapTemplateWriteError(err); err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	return nil
}

// Delete removes a template and its encrypted user data.
func (r *MongoDeploymentTemplateRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	return nil
}

// ReplaceUserData seals and replaces a template's cloud-init.
func (r *MongoDeploymentTemplateRepo) ReplaceUserData(ctx context.Context, id, userData string) error {
	sealed, err := r.sealer.Seal(userData)
	if err != nil {
		return err
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"sealedUserData": sealed,
		"updatedAt":      time.Now().UTC(),
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	return nil
}

// ClearUserData removes cloud-init without changing the rest of the template.
func (r *MongoDeploymentTemplateRepo) ClearUserData(ctx context.Context, id string) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$unset": bson.M{"sealedUserData": ""},
		"$set":   bson.M{"updatedAt": time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	return nil
}

// UserData explicitly decrypts cloud-init for deployment and has no API read path.
func (r *MongoDeploymentTemplateRepo) UserData(ctx context.Context, id string) (string, error) {
	var doc deploymentTemplateDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id},
		options.FindOne().SetProjection(bson.M{"sealedUserData": 1})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", provisioningdomain.ErrDeploymentTemplateNotFound
	}
	if err != nil {
		return "", err
	}
	if doc.SealedUserData == "" {
		return "", provisioningdomain.ErrDeploymentTemplateUserDataMissing
	}
	return r.sealer.Open(doc.SealedUserData)
}

// CountByIntegration prevents deleting an integration referenced by templates.
func (r *MongoDeploymentTemplateRepo) CountByIntegration(ctx context.Context, integrationID string) (int, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"integrationId": integrationID})
	return int(count), err
}

func normalizeTemplateName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func mapTemplateWriteError(err error) error {
	if mongo.IsDuplicateKeyError(err) {
		return provisioningdomain.ErrDeploymentTemplateNameTaken
	}
	return err
}

func toDeploymentTemplate(doc *deploymentTemplateDoc) *provisioningdomain.DeploymentTemplate {
	return &provisioningdomain.DeploymentTemplate{
		ID:            doc.ID,
		IntegrationID: doc.IntegrationID,
		Name:          doc.Name,
		Description:   doc.Description,
		ImageID:       doc.ImageID,
		Ephemeral:     doc.Ephemeral,
		HasUserData:   doc.SealedUserData != "",
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     doc.UpdatedAt,
	}
}

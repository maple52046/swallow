package localrepo

import (
	"context"
	"errors"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

type sourceDoc struct {
	Name          string   `bson:"name"`
	Format        string   `bson:"format"`
	URL           string   `bson:"url"`
	Suites        []string `bson:"suites,omitempty"`
	Components    []string `bson:"components,omitempty"`
	Architectures []string `bson:"architectures,omitempty"`
}

type fileDoc struct {
	Path   string `bson:"path"`
	URL    string `bson:"url"`
	SHA256 string `bson:"sha256"`
}

// observedSourceDoc is a source as it was ensured and what the provider reported; the reported name
// and format are the source's own, so they are not stored twice.
type observedSourceDoc struct {
	Source       sourceDoc  `bson:"source"`
	State        string     `bson:"state"`
	Message      string     `bson:"message,omitempty"`
	LastSyncedAt *time.Time `bson:"lastSyncedAt,omitempty"`
	ContentCount int        `bson:"contentCount"`
}

type observedFileDoc struct {
	File      fileDoc `bson:"file"`
	State     string  `bson:"state"`
	Message   string  `bson:"message,omitempty"`
	SizeBytes int64   `bson:"sizeBytes"`
}

// observedImageDoc is a declared image as it was ensured (its reference) and what the provider
// reported.
type observedImageDoc struct {
	Reference string `bson:"reference"`
	State     string `bson:"state"`
	Message   string `bson:"message,omitempty"`
	SizeBytes int64  `bson:"sizeBytes"`
}

type debSourceDoc struct {
	Name       string   `bson:"name"`
	URI        string   `bson:"uri"`
	Suites     []string `bson:"suites,omitempty"`
	Components []string `bson:"components,omitempty"`
}

// registryMirrorDoc is one upstream registry's mirror. Mirrors are stored as a list rather than a
// map because registry hosts contain dots, which a document field name must not.
type registryMirrorDoc struct {
	Host   string `bson:"host"`
	Mirror string `bson:"mirror"`
}

type endpointsDoc struct {
	Deb             []debSourceDoc      `bson:"deb,omitempty"`
	DebSigningKey   string              `bson:"debSigningKey,omitempty"`
	Registry        string              `bson:"registry,omitempty"`
	RegistryMirrors []registryMirrorDoc `bson:"registryMirrors,omitempty"`
	PythonIndex     string              `bson:"pythonIndex,omitempty"`
	Files           string              `bson:"files,omitempty"`
}

// statusDoc is the controller's status. A record written before images were observed items holds
// a prefetchedImages list instead of images; decoding ignores that field, so such a record reads as
// no image observed yet, and the controller's next status write drops it.
type statusDoc struct {
	IntegrationID      string                          `bson:"integrationId,omitempty"`
	ObservedGeneration int64                           `bson:"observedGeneration"`
	Sources            []observedSourceDoc             `bson:"sources,omitempty"`
	Files              []observedFileDoc               `bson:"files,omitempty"`
	Images             []observedImageDoc              `bson:"images,omitempty"`
	Endpoints          *endpointsDoc                   `bson:"endpoints,omitempty"`
	ObservedAt         *time.Time                      `bson:"observedAt,omitempty"`
	Conditions         []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
}

// specDoc is the user's spec. A record written before serveDeployments existed has no such field
// and decodes as false, the default, so those Sites fetch from the upstreams until a user turns
// serving on.
type specDoc struct {
	ServeDeployments bool        `bson:"serveDeployments"`
	Sources          []sourceDoc `bson:"sources"`
}

// localRepositoryDoc is one Site's record, keyed by the Site id.
type localRepositoryDoc struct {
	SiteID     string    `bson:"_id"`
	Generation int64     `bson:"generation"`
	Spec       specDoc   `bson:"spec"`
	Status     statusDoc `bson:"status"`
}

// MongoLocalRepositoryRepo stores Local Repository records in `local_repositories`.
//
// The spec and the status have separate writers and are written with separate single-document
// updates: PutSpec ($set spec, $inc generation, conditional on the generation) and SetStatus ($set
// status). Neither reads before it writes, so a concurrent write of the other half is never lost.
type MongoLocalRepositoryRepo struct {
	col *mongo.Collection
}

var _ localrepodomain.Store = (*MongoLocalRepositoryRepo)(nil)

// NewMongoLocalRepositoryRepo returns the repository; the Site id is the document id, so no index is
// needed.
func NewMongoLocalRepositoryRepo(db *mongo.Database) *MongoLocalRepositoryRepo {
	return &MongoLocalRepositoryRepo{col: db.Collection(localrepodomain.Collection)}
}

// Get implements localrepodomain.Store.
func (r *MongoLocalRepositoryRepo) Get(ctx context.Context, siteID string) (*localrepodomain.LocalRepository, error) {
	var doc localRepositoryDoc
	err := r.col.FindOne(ctx, bson.M{"_id": siteID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, localrepodomain.ErrLocalRepositoryNotFound
	}
	if err != nil {
		return nil, err
	}
	return toLocalRepository(&doc), nil
}

// List implements localrepodomain.Store.
func (r *MongoLocalRepositoryRepo) List(ctx context.Context) ([]*localrepodomain.LocalRepository, error) {
	cursor, err := r.col.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var docs []localRepositoryDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*localrepodomain.LocalRepository, len(docs))
	for i := range docs {
		out[i] = toLocalRepository(&docs[i])
	}
	return out, nil
}

// PutSpec implements localrepodomain.Store with one conditional update matching the expected
// generation. Expecting 0 upserts, so the first write creates the record; when a record already
// exists at another generation, that upsert's insert hits the Site id and is a conflict like a
// mismatched update.
func (r *MongoLocalRepositoryRepo) PutSpec(ctx context.Context, siteID string, expectedGeneration int64, spec localrepodomain.Spec) (*localrepodomain.LocalRepository, error) {
	var doc localRepositoryDoc
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": siteID, "generation": expectedGeneration},
		bson.M{
			"$set": bson.M{"spec": toSpecDoc(spec)},
			"$inc": bson.M{"generation": 1},
		},
		options.FindOneAndUpdate().SetUpsert(expectedGeneration == 0).SetReturnDocument(options.After),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
		return nil, localrepodomain.ErrGenerationConflict
	}
	if err != nil {
		return nil, err
	}
	return toLocalRepository(&doc), nil
}

// SetStatus implements localrepodomain.Store; a missing record is created at generation 0 with an
// empty spec, which leaves the next spec write expecting generation 0.
func (r *MongoLocalRepositoryRepo) SetStatus(ctx context.Context, siteID string, status localrepodomain.Status) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": siteID},
		bson.M{
			"$set":         bson.M{"status": toStatusDoc(status)},
			"$setOnInsert": bson.M{"generation": int64(0), "spec": specDoc{Sources: []sourceDoc{}}},
		},
		options.Update().SetUpsert(true))
	return err
}

// Delete implements localrepodomain.Store.
func (r *MongoLocalRepositoryRepo) Delete(ctx context.Context, siteID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": siteID})
	return err
}

func toSpecDoc(spec localrepodomain.Spec) specDoc {
	doc := specDoc{ServeDeployments: spec.ServeDeployments, Sources: make([]sourceDoc, 0, len(spec.Sources))}
	for _, source := range spec.Sources {
		doc.Sources = append(doc.Sources, toSourceDoc(source))
	}
	return doc
}

func toSourceDoc(source localrepodomain.Source) sourceDoc {
	return sourceDoc{
		Name: source.Name, Format: string(source.Format), URL: source.URL,
		Suites: source.Suites, Components: source.Components, Architectures: source.Architectures,
	}
}

func fromSourceDoc(doc sourceDoc) localrepodomain.Source {
	return localrepodomain.Source{
		Name: doc.Name, Format: localrepodomain.Format(doc.Format), URL: doc.URL,
		Suites: emptyToNil(doc.Suites), Components: emptyToNil(doc.Components), Architectures: emptyToNil(doc.Architectures),
	}
}

func toStatusDoc(status localrepodomain.Status) statusDoc {
	doc := statusDoc{
		IntegrationID: status.IntegrationID, ObservedGeneration: status.ObservedGeneration, ObservedAt: utc(status.ObservedAt),
	}
	if len(status.Conditions) > 0 {
		doc.Conditions = convergencestore.ConditionDocs(status.Conditions)
	}
	for _, observed := range status.Sources {
		doc.Sources = append(doc.Sources, observedSourceDoc{
			Source: toSourceDoc(observed.Source), State: string(observed.Status.State), Message: observed.Status.Message,
			LastSyncedAt: utc(observed.Status.LastSyncedAt), ContentCount: observed.Status.ContentCount,
		})
	}
	for _, observed := range status.Files {
		doc.Files = append(doc.Files, observedFileDoc{
			File:  fileDoc{Path: observed.File.Path, URL: observed.File.URL, SHA256: observed.File.SHA256},
			State: string(observed.Status.State), Message: observed.Status.Message, SizeBytes: observed.Status.SizeBytes,
		})
	}
	for _, observed := range status.Images {
		doc.Images = append(doc.Images, observedImageDoc{
			Reference: observed.Reference, State: string(observed.Status.State), Message: observed.Status.Message,
			SizeBytes: observed.Status.SizeBytes,
		})
	}
	if endpoints := status.Endpoints; endpoints != nil {
		doc.Endpoints = &endpointsDoc{
			DebSigningKey: endpoints.DebSigningKey, Registry: endpoints.Registry,
			PythonIndex: endpoints.PythonIndex, Files: endpoints.Files,
		}
		for _, deb := range endpoints.Deb {
			doc.Endpoints.Deb = append(doc.Endpoints.Deb, debSourceDoc{Name: deb.Name, URI: deb.URI, Suites: deb.Suites, Components: deb.Components})
		}
		for host, mirror := range endpoints.RegistryMirrors {
			doc.Endpoints.RegistryMirrors = append(doc.Endpoints.RegistryMirrors, registryMirrorDoc{Host: host, Mirror: mirror})
		}
		sort.Slice(doc.Endpoints.RegistryMirrors, func(i, j int) bool {
			return doc.Endpoints.RegistryMirrors[i].Host < doc.Endpoints.RegistryMirrors[j].Host
		})
	}
	return doc
}

func toLocalRepository(doc *localRepositoryDoc) *localrepodomain.LocalRepository {
	record := &localrepodomain.LocalRepository{SiteID: doc.SiteID, Generation: doc.Generation}
	record.Spec.ServeDeployments = doc.Spec.ServeDeployments
	for _, source := range doc.Spec.Sources {
		record.Spec.Sources = append(record.Spec.Sources, fromSourceDoc(source))
	}
	status := doc.Status
	record.Status = localrepodomain.Status{
		IntegrationID: status.IntegrationID, ObservedGeneration: status.ObservedGeneration, ObservedAt: status.ObservedAt,
	}
	if len(status.Conditions) > 0 {
		record.Status.Conditions = convergencestore.ToConditions(status.Conditions)
	}
	for _, observed := range status.Sources {
		source := fromSourceDoc(observed.Source)
		record.Status.Sources = append(record.Status.Sources, localrepodomain.ObservedSource{Source: source, Status: localrepodomain.SourceStatus{
			Name: source.Name, Format: source.Format, State: localrepodomain.SyncState(observed.State), Message: observed.Message,
			LastSyncedAt: observed.LastSyncedAt, ContentCount: observed.ContentCount,
		}})
	}
	for _, observed := range status.Files {
		file := localrepodomain.File{Path: observed.File.Path, URL: observed.File.URL, SHA256: observed.File.SHA256}
		record.Status.Files = append(record.Status.Files, localrepodomain.ObservedFile{File: file, Status: localrepodomain.FileStatus{
			Path: file.Path, State: localrepodomain.SyncState(observed.State), Message: observed.Message, SizeBytes: observed.SizeBytes,
		}})
	}
	for _, observed := range status.Images {
		record.Status.Images = append(record.Status.Images, localrepodomain.ObservedImage{Reference: observed.Reference, Status: localrepodomain.ImageStatus{
			Reference: observed.Reference, State: localrepodomain.SyncState(observed.State), Message: observed.Message, SizeBytes: observed.SizeBytes,
		}})
	}
	if endpoints := status.Endpoints; endpoints != nil {
		record.Status.Endpoints = &localrepodomain.HostEndpoints{
			DebSigningKey: endpoints.DebSigningKey, Registry: endpoints.Registry,
			PythonIndex: endpoints.PythonIndex, Files: endpoints.Files,
		}
		for _, deb := range endpoints.Deb {
			record.Status.Endpoints.Deb = append(record.Status.Endpoints.Deb, localrepodomain.DebSource{
				Name: deb.Name, URI: deb.URI, Suites: emptyToNil(deb.Suites), Components: emptyToNil(deb.Components),
			})
		}
		if len(endpoints.RegistryMirrors) > 0 {
			record.Status.Endpoints.RegistryMirrors = map[string]string{}
			for _, mirror := range endpoints.RegistryMirrors {
				record.Status.Endpoints.RegistryMirrors[mirror.Host] = mirror.Mirror
			}
		}
	}
	return record
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := t.UTC()
	return &at
}

func emptyToNil(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

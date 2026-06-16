package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"senioguard-documents/internal/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Repositories struct {
	Collections *CollectionRepository
	Documents   *DocumentRepository
}

func New(db *mongo.Database) *Repositories {
	return &Repositories{
		Collections: &CollectionRepository{col: db.Collection("collections")},
		Documents:   &DocumentRepository{col: db.Collection("documents")},
	}
}

func (r *Repositories) EnsureIndexes(ctx context.Context) error {
	if _, err := r.Collections.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "parentId", Value: 1}, {Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	_, err := r.Documents.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "collectionId", Value: 1}, {Key: "displayName", Value: 1}}},
		{Keys: bson.D{{Key: "externalId", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
		{Keys: bson.D{{Key: "displayName", Value: "text"}, {Key: "text", Value: "text"}, {Key: "labels", Value: "text"}, {Key: "summary", Value: "text"}}},
	})
	return err
}

type CollectionRepository struct {
	col *mongo.Collection
}

func (r *CollectionRepository) Create(ctx context.Context, name string, parentID *primitive.ObjectID) (model.Collection, error) {
	now := time.Now().UTC()
	c := model.Collection{Name: name, ParentID: parentID, Path: name, CreatedAt: now, UpdatedAt: now}
	if parentID != nil {
		parent, err := r.Get(ctx, *parentID)
		if err != nil {
			return c, err
		}
		c.Path = strings.TrimRight(parent.Path, "/") + "/" + name
	}
	result, err := r.col.InsertOne(ctx, c)
	if err != nil {
		return c, err
	}
	c.ID = result.InsertedID.(primitive.ObjectID)
	return c, nil
}

func (r *CollectionRepository) Get(ctx context.Context, id primitive.ObjectID) (model.Collection, error) {
	var c model.Collection
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	return c, err
}

func (r *CollectionRepository) FindChild(ctx context.Context, parentID *primitive.ObjectID, name string) (model.Collection, error) {
	filter := bson.M{"parentId": parentID, "name": name}
	if parentID == nil {
		filter = bson.M{"parentId": bson.M{"$exists": false}, "name": name}
	}
	var c model.Collection
	err := r.col.FindOne(ctx, filter).Decode(&c)
	return c, err
}

func (r *CollectionRepository) Update(ctx context.Context, id primitive.ObjectID, name string) (model.Collection, error) {
	now := time.Now().UTC()
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$set": bson.M{"name": name, "updatedAt": now}})
	if err != nil {
		return model.Collection{}, err
	}
	return r.Get(ctx, id)
}

func (r *CollectionRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *CollectionRepository) ListChildren(ctx context.Context, parentID *primitive.ObjectID) ([]model.Collection, error) {
	filter := bson.M{"parentId": parentID}
	if parentID == nil {
		filter = bson.M{"parentId": bson.M{"$exists": false}}
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []model.Collection
	return items, cursor.All(ctx, &items)
}

func (r *CollectionRepository) ListAll(ctx context.Context) ([]model.Collection, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "path", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []model.Collection
	return items, cursor.All(ctx, &items)
}

type DocumentRepository struct {
	col *mongo.Collection
}

func (r *DocumentRepository) Create(ctx context.Context, d model.Document) (model.Document, error) {
	now := time.Now().UTC()
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.DisplayName = r.nextAvailableName(ctx, d.CollectionID, d.DisplayName)
	d.Status = model.StatusPending
	d.CreatedAt = now
	d.UpdatedAt = now
	result, err := r.col.InsertOne(ctx, d)
	if err != nil {
		return d, err
	}
	d.ID = result.InsertedID.(primitive.ObjectID)
	return d, nil
}

func (r *DocumentRepository) UpsertExternal(ctx context.Context, d model.Document) (model.Document, bool, error) {
	if d.ExternalID == "" {
		return model.Document{}, false, fmt.Errorf("external id is required")
	}
	now := time.Now().UTC()
	var existing model.Document
	err := r.col.FindOne(ctx, bson.M{"externalId": d.ExternalID}).Decode(&existing)
	if err == nil {
		update := bson.M{
			"collectionId": existing.CollectionID,
			"displayName":  d.DisplayName,
			"storageKey":   d.StorageKey,
			"mime":         d.MIME,
			"size":         d.Size,
			"status":       model.StatusPending,
			"text":         "",
			"error":        "",
			"source":       d.Source,
			"sourceType":   d.SourceType,
			"externalUrl":  d.ExternalURL,
			"repository":   d.Repository,
			"author":       d.Author,
			"updatedAt":    now,
		}
		if d.CollectionID != nil {
			update["collectionId"] = d.CollectionID
			existing.CollectionID = d.CollectionID
		}
		_, err = r.col.UpdateByID(ctx, existing.ID, bson.M{"$set": update})
		if err != nil {
			return existing, false, err
		}
		updated, err := r.Get(ctx, existing.ID)
		return updated, false, err
	}
	if err != mongo.ErrNoDocuments {
		return model.Document{}, false, err
	}
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.Status = model.StatusPending
	d.CreatedAt = now
	d.UpdatedAt = now
	result, err := r.col.InsertOne(ctx, d)
	if err != nil {
		return d, false, err
	}
	d.ID = result.InsertedID.(primitive.ObjectID)
	return d, true, nil
}

func (r *DocumentRepository) Get(ctx context.Context, id primitive.ObjectID) (model.Document, error) {
	var d model.Document
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	return d, err
}

func (r *DocumentRepository) ListByCollection(ctx context.Context, collectionID *primitive.ObjectID) ([]model.Document, error) {
	filter := bson.M{"collectionId": collectionID}
	if collectionID == nil {
		filter = bson.M{"collectionId": bson.M{"$exists": false}}
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "displayName", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []model.Document
	return docs, cursor.All(ctx, &docs)
}

func (r *DocumentRepository) ListAll(ctx context.Context) ([]model.Document, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "displayName", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []model.Document
	return docs, cursor.All(ctx, &docs)
}

func (r *DocumentRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status model.DocumentStatus, message string) error {
	update := bson.M{"status": status, "updatedAt": time.Now().UTC()}
	if message != "" {
		update["error"] = message
	}
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$set": update})
	return err
}

func (r *DocumentRepository) UpdateProcessed(ctx context.Context, id primitive.ObjectID, text, summary, category string, labels []string) error {
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$set": bson.M{
		"status":    model.StatusProcessed,
		"text":      text,
		"summary":   summary,
		"category":  category,
		"labels":    labels,
		"error":     "",
		"updatedAt": time.Now().UTC(),
	}})
	return err
}

func (r *DocumentRepository) Move(ctx context.Context, id primitive.ObjectID, collectionID *primitive.ObjectID) error {
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$set": bson.M{"collectionId": collectionID, "updatedAt": time.Now().UTC()}})
	return err
}

func (r *DocumentRepository) SetMetadata(ctx context.Context, id primitive.ObjectID, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("metadata key is required")
	}
	if strings.ContainsAny(key, ".$") {
		return fmt.Errorf("metadata key cannot contain . or $")
	}
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$set": bson.M{
		"metadata." + key: value,
		"updatedAt":       time.Now().UTC(),
	}})
	return err
}

func (r *DocumentRepository) DeleteMetadata(ctx context.Context, id primitive.ObjectID, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("metadata key is required")
	}
	if strings.ContainsAny(key, ".$") {
		return fmt.Errorf("metadata key cannot contain . or $")
	}
	_, err := r.col.UpdateByID(ctx, id, bson.M{
		"$unset": bson.M{"metadata." + key: ""},
		"$set":   bson.M{"updatedAt": time.Now().UTC()},
	})
	return err
}

func (r *DocumentRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *DocumentRepository) Search(ctx context.Context, q string, collectionID *primitive.ObjectID, limit int64) ([]model.Document, error) {
	filter := bson.M{"$text": bson.M{"$search": q}}
	if collectionID != nil {
		filter["collectionId"] = collectionID
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetLimit(limit).SetProjection(bson.M{"score": bson.M{"$meta": "textScore"}}).SetSort(bson.M{"score": bson.M{"$meta": "textScore"}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []model.Document
	return docs, cursor.All(ctx, &docs)
}

func (r *DocumentRepository) nextAvailableName(ctx context.Context, collectionID *primitive.ObjectID, name string) string {
	if name == "" {
		name = "document"
	}
	base, ext := splitExt(name)
	filter := bson.M{"collectionId": collectionID, "displayName": primitive.Regex{Pattern: "^" + regexp.QuoteMeta(base) + `(_[0-9]+)?` + regexp.QuoteMeta(ext) + "$"}}
	cursor, err := r.col.Find(ctx, filter)
	if err != nil {
		return name
	}
	defer cursor.Close(ctx)
	used := map[string]bool{}
	for cursor.Next(ctx) {
		var d model.Document
		if cursor.Decode(&d) == nil {
			used[d.DisplayName] = true
		}
	}
	if !used[name] {
		return name
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !used[candidate] {
			return candidate
		}
	}
}

func splitExt(name string) (string, string) {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 {
		return name, ""
	}
	return name[:idx], name[idx:]
}

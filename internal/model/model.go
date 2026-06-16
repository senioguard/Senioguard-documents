package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Collection struct {
	ID        primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	Name      string              `bson:"name" json:"name"`
	ParentID  *primitive.ObjectID `bson:"parentId,omitempty" json:"parentId,omitempty"`
	Path      string              `bson:"path" json:"path"`
	CreatedAt time.Time           `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time           `bson:"updatedAt" json:"updatedAt"`
}

type DocumentStatus string

const (
	StatusPending    DocumentStatus = "pending"
	StatusProcessing DocumentStatus = "processing"
	StatusProcessed  DocumentStatus = "processed"
	StatusError      DocumentStatus = "error"
)

type Document struct {
	ID           primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	CollectionID *primitive.ObjectID `bson:"collectionId,omitempty" json:"collectionId,omitempty"`
	DisplayName  string              `bson:"displayName" json:"displayName"`
	StorageKey   string              `bson:"storageKey" json:"storageKey"`
	MIME         string              `bson:"mime" json:"mime"`
	Size         int64               `bson:"size" json:"size"`
	Status       DocumentStatus      `bson:"status" json:"status"`
	Labels       []string            `bson:"labels,omitempty" json:"labels,omitempty"`
	Category     string              `bson:"category,omitempty" json:"category,omitempty"`
	Summary      string              `bson:"summary,omitempty" json:"summary,omitempty"`
	Text         string              `bson:"text,omitempty" json:"text,omitempty"`
	Error        string              `bson:"error,omitempty" json:"error,omitempty"`
	Source       string              `bson:"source,omitempty" json:"source,omitempty"`
	SourceType   string              `bson:"sourceType,omitempty" json:"sourceType,omitempty"`
	ExternalID   string              `bson:"externalId,omitempty" json:"externalId,omitempty"`
	ExternalURL  string              `bson:"externalUrl,omitempty" json:"externalUrl,omitempty"`
	Repository   string              `bson:"repository,omitempty" json:"repository,omitempty"`
	Author       string              `bson:"author,omitempty" json:"author,omitempty"`
	CreatedAt    time.Time           `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time           `bson:"updatedAt" json:"updatedAt"`
}

type Chunk struct {
	DocumentID   primitive.ObjectID  `bson:"documentId" json:"documentId"`
	CollectionID *primitive.ObjectID `bson:"collectionId,omitempty" json:"collectionId,omitempty"`
	DisplayName  string              `bson:"displayName" json:"displayName"`
	ChunkIndex   int                 `bson:"chunkIndex" json:"chunkIndex"`
	Text         string              `bson:"text" json:"text"`
}

type RAGSource struct {
	DocumentID  primitive.ObjectID `json:"documentId"`
	DisplayName string             `json:"displayName"`
	ChunkIndex  int                `json:"chunkIndex"`
	Text        string             `json:"text"`
	Score       float32            `json:"score,omitempty"`
}

type RAGResponse struct {
	Answer  string      `json:"answer"`
	Sources []RAGSource `json:"sources"`
}

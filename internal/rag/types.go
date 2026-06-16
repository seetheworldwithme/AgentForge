package rag

import "time"

type KnowledgeBase struct {
	ID             string
	Name           string
	EmbeddingModel string
	ChunkSize      int
	Overlap        int
	CreatedAt      time.Time
}

type Document struct {
	ID          string
	KBID        string
	FilePath    string
	FileType    string
	ChunkCount  int
	Status      string
	ErrorMsg    string
	ContentHash string
	CreatedAt   time.Time
}

type Chunk struct {
	ID          int64
	DocID       string
	KBID        string
	Content     string
	HeadingPath string
	Source      string
	TokenCount  int
	Seq         int
}

type ScoredChunk struct {
	Chunk
	Score float64
}

type RawDocument struct {
	FilePath string
	Content  []byte
	FileType string
}

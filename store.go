package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// ChunkMeta holds the metadata for one doc chunk as stored in metadata.json.
type ChunkMeta struct {
	Text       string `json:"text"`
	Page       int    `json:"page"`
	Section    string `json:"section"`
	ChunkIndex int    `json:"chunk_index"`
}

// SearchResult is returned by VectorStore.Search.
type SearchResult struct {
	Text    string  `json:"text"`
	Page    int     `json:"page"`
	Section string  `json:"section"`
	Score   float64 `json:"score"`
}

// VectorStore holds the flat in-memory vector index.
type VectorStore struct {
	vectors [][]float32
	meta    []ChunkMeta
	dim     int
}

// LoadVectorStore parses vectors.bin and metadata.json (both in-memory bytes).
//
// vectors.bin format (little-endian):
//
//	int32  N  — number of chunks
//	int32  D  — embedding dimension
//	N*D float32 — row-major embeddings
func LoadVectorStore(vectorsBin, metadataJSON []byte) (*VectorStore, error) {
	if len(vectorsBin) < 8 {
		return nil, fmt.Errorf("vectors.bin too short (%d bytes)", len(vectorsBin))
	}

	n := int(int32(binary.LittleEndian.Uint32(vectorsBin[0:4])))
	d := int(int32(binary.LittleEndian.Uint32(vectorsBin[4:8])))

	expectedBytes := 8 + n*d*4
	if len(vectorsBin) < expectedBytes {
		return nil, fmt.Errorf("vectors.bin: expected %d bytes, got %d", expectedBytes, len(vectorsBin))
	}

	vecs := make([][]float32, n)
	offset := 8
	for i := range vecs {
		row := make([]float32, d)
		for j := range row {
			bits := binary.LittleEndian.Uint32(vectorsBin[offset:])
			row[j] = math.Float32frombits(bits)
			offset += 4
		}
		vecs[i] = row
	}

	var meta []ChunkMeta
	if err := json.Unmarshal(metadataJSON, &meta); err != nil {
		return nil, fmt.Errorf("metadata.json: %w", err)
	}
	if len(meta) != n {
		return nil, fmt.Errorf("metadata.json has %d entries but vectors.bin has %d", len(meta), n)
	}

	return &VectorStore{vectors: vecs, meta: meta, dim: d}, nil
}

func (s *VectorStore) Dim() int { return s.dim }
func (s *VectorStore) Len() int { return len(s.vectors) }

// Search returns the top-k chunks by cosine similarity.
func (s *VectorStore) Search(query []float32, topK int) ([]SearchResult, error) {
	if len(query) != s.dim {
		return nil, fmt.Errorf("query dim %d ≠ store dim %d", len(query), s.dim)
	}
	if topK <= 0 {
		topK = 5
	}
	if topK > len(s.vectors) {
		topK = len(s.vectors)
	}

	qn := l2Norm(query)
	if qn == 0 {
		return nil, fmt.Errorf("query embedding is a zero vector")
	}
	qNorm := make([]float32, len(query))
	for i, v := range query {
		qNorm[i] = v / qn
	}

	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, len(s.vectors))
	for i, vec := range s.vectors {
		scores[i] = scored{idx: i, score: dot(qNorm, vec)}
	}

	sort.Slice(scores, func(a, b int) bool {
		return scores[a].score > scores[b].score
	})

	results := make([]SearchResult, topK)
	for i := range results {
		c := scores[i]
		m := s.meta[c.idx]
		results[i] = SearchResult{
			Text:    m.Text,
			Page:    m.Page,
			Section: m.Section,
			Score:   float64(c.score),
		}
	}
	return results, nil
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func l2Norm(v []float32) float32 {
	var s float32
	for _, x := range v {
		s += x * x
	}
	return float32(math.Sqrt(float64(s)))
}

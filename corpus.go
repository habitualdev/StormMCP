package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// CleanChunk is one entry from corpus/clean_chunks.json.
type CleanChunk struct {
	Text      string `json:"text"`
	File      string `json:"file"`
	ChunkType string `json:"chunk_type"`
	Source    string `json:"source"`
}

// ExampleResult is returned by CorpusSearch.
type ExampleResult struct {
	Text      string  `json:"text"`
	File      string  `json:"file"`
	ChunkType string  `json:"chunk_type"`
	Score     float64 `json:"score"`
}

// CorpusSearcher provides keyword search over clean Storm examples using TF-IDF.
type CorpusSearcher struct {
	chunks []CleanChunk
	// term → {docIndex → tf-idf weight}
	index map[string]map[int]float64
	idf   map[string]float64
}

// NewCorpusSearcher builds the searcher from the embedded JSON bytes.
func NewCorpusSearcher(data []byte) (*CorpusSearcher, error) {
	var chunks []CleanChunk
	if err := json.Unmarshal(data, &chunks); err != nil {
		return nil, fmt.Errorf("parse clean_chunks.json: %w", err)
	}

	cs := &CorpusSearcher{
		chunks: chunks,
		index:  make(map[string]map[int]float64),
		idf:    make(map[string]float64),
	}
	cs.build()
	return cs, nil
}

// Len returns the number of indexed examples.
func (cs *CorpusSearcher) Len() int { return len(cs.chunks) }

func (cs *CorpusSearcher) build() {
	n := len(cs.chunks)
	if n == 0 {
		return
	}

	// Term frequency per document
	tf := make([]map[string]float64, n)
	df := make(map[string]int)

	for i, c := range cs.chunks {
		terms := tokenizeText(c.Text)
		counts := make(map[string]int, len(terms))
		for _, t := range terms {
			counts[t]++
		}
		tf[i] = make(map[string]float64, len(counts))
		maxCount := 1
		for _, cnt := range counts {
			if cnt > maxCount {
				maxCount = cnt
			}
		}
		for term, cnt := range counts {
			// Sub-linear TF (mirrors sklearn's sublinear_tf=True)
			tf[i][term] = 1 + math.Log(float64(cnt))
			df[term]++
		}
	}

	// IDF with smooth (sklearn default: log((1+n)/(1+df))+1)
	for term, docFreq := range df {
		cs.idf[term] = math.Log(float64(1+n)/float64(1+docFreq)) + 1.0
	}

	// Build inverted index with TF-IDF weights
	for i, tfDoc := range tf {
		for term, tfVal := range tfDoc {
			idfVal := cs.idf[term]
			weight := tfVal * idfVal
			if cs.index[term] == nil {
				cs.index[term] = make(map[int]float64)
			}
			cs.index[term][i] = weight
		}
	}
}

// Search returns the top-k clean examples most relevant to the query.
func (cs *CorpusSearcher) Search(query string, topK int) []ExampleResult {
	if len(cs.chunks) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 3
	}

	queryTerms := tokenizeText(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Compute query TF-IDF vector
	qCounts := make(map[string]int)
	for _, t := range queryTerms {
		qCounts[t]++
	}
	qVec := make(map[string]float64, len(qCounts))
	for term, cnt := range qCounts {
		idfVal, ok := cs.idf[term]
		if !ok {
			continue
		}
		qVec[term] = (1 + math.Log(float64(cnt))) * idfVal
	}

	// Cosine similarity via dot product over index
	scores := make(map[int]float64)
	for term, qw := range qVec {
		if postings, ok := cs.index[term]; ok {
			for docIdx, dw := range postings {
				scores[docIdx] += qw * dw
			}
		}
	}

	type pair struct {
		idx   int
		score float64
	}
	pairs := make([]pair, 0, len(scores))
	for idx, score := range scores {
		if score > 0 {
			pairs = append(pairs, pair{idx, score})
		}
	}
	sort.Slice(pairs, func(a, b int) bool {
		return pairs[a].score > pairs[b].score
	})

	if topK > len(pairs) {
		topK = len(pairs)
	}
	results := make([]ExampleResult, topK)
	for i, p := range pairs[:topK] {
		c := cs.chunks[p.idx]
		results[i] = ExampleResult{
			Text:      c.Text,
			File:      c.File,
			ChunkType: c.ChunkType,
			Score:     math.Round(p.score*1e4) / 1e4,
		}
	}
	return results
}

// SummarizeResults extracts a compact data-model hint from retrieved corpus chunks.
// Returns a one-line string like "forms: inet:fqdn, inet:ipv4 | edges: inet:fqdn -> inet:ipv4 | tags: #mal"
// suitable for prepending to the model prompt.
func SummarizeResults(results []ExampleResult) string {
	reForm := regexp.MustCompile(`\b[a-z_][a-z0-9_]*(?::[a-z0-9_]+)+(?:[:.][a-z0-9_]+)*\b`)
	reEdge := regexp.MustCompile(`([a-z_][a-z0-9_]*(?::[a-z0-9_]+)+)\s*->\s*([a-z_][a-z0-9_]*(?::[a-z0-9_]+)+)`)
	reTag := regexp.MustCompile(`#([a-z_][a-z0-9_.]+)`)

	formSeen := make(map[string]bool)
	edgeSeen := make(map[string]bool)
	tagSeen := make(map[string]bool)
	var forms, edges, tags []string

	addUniq := func(m map[string]bool, list *[]string, v string) {
		if !m[v] {
			m[v] = true
			*list = append(*list, v)
		}
	}

	for _, r := range results {
		for _, m := range reForm.FindAllString(r.Text, -1) {
			addUniq(formSeen, &forms, m)
		}
		for _, m := range reEdge.FindAllStringSubmatch(r.Text, -1) {
			addUniq(edgeSeen, &edges, m[1]+" -> "+m[2])
		}
		for _, m := range reTag.FindAllStringSubmatch(r.Text, -1) {
			addUniq(tagSeen, &tags, "#"+m[1])
		}
	}

	if len(forms) > 6 {
		forms = forms[:6]
	}
	if len(edges) > 3 {
		edges = edges[:3]
	}
	if len(tags) > 4 {
		tags = tags[:4]
	}

	var parts []string
	if len(forms) > 0 {
		parts = append(parts, "forms: "+strings.Join(forms, ", "))
	}
	if len(edges) > 0 {
		parts = append(parts, "edges: "+strings.Join(edges, "; "))
	}
	if len(tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(tags, ", "))
	}
	return strings.Join(parts, " | ")
}

// tokenizeText splits text into lowercase unigrams and bigrams.
func tokenizeText(text string) []string {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9') && r != '_' && r != '$' && r != '.'
	})

	if len(fields) == 0 {
		return nil
	}

	tokens := make([]string, 0, len(fields)*2)
	for i, f := range fields {
		if len(f) > 1 {
			tokens = append(tokens, f)
		}
		if i > 0 && len(fields[i-1]) > 1 && len(f) > 1 {
			tokens = append(tokens, fields[i-1]+" "+f)
		}
	}
	return tokens
}

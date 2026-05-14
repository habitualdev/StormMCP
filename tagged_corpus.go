package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// TaggedQuery is one entry from corpus/tagged_queries.json.
type TaggedQuery struct {
	Text          string   `json:"text"`
	File          string   `json:"file"`
	ChunkType     string   `json:"chunk_type"`
	Description   string   `json:"description"`
	ResultTags    []string `json:"result_tags"`
	TechniqueTags []string `json:"technique_tags"`
}

// TaggedQueryResult is returned by TaggedCorpusSearcher.Search.
type TaggedQueryResult struct {
	Text          string   `json:"text"`
	File          string   `json:"file"`
	Description   string   `json:"description"`
	ResultTags    []string `json:"result_tags"`
	TechniqueTags []string `json:"technique_tags"`
	Score         float64  `json:"score"`
}

// TaggedCorpusSearcher indexes tagged Storm queries for tag-aware search.
type TaggedCorpusSearcher struct {
	queries []TaggedQuery
	// TF-IDF index over the combined search text (description + tags + code)
	index map[string]map[int]float64
	idf   map[string]float64
	// Tag index: tag string → set of doc indices
	resultTagIdx    map[string][]int
	techniqueTagIdx map[string][]int
}

// NewTaggedCorpusSearcher builds the searcher from tagged_queries.json bytes.
func NewTaggedCorpusSearcher(data []byte) (*TaggedCorpusSearcher, error) {
	var queries []TaggedQuery
	if err := json.Unmarshal(data, &queries); err != nil {
		return nil, fmt.Errorf("parse tagged_queries.json: %w", err)
	}

	cs := &TaggedCorpusSearcher{
		queries:         queries,
		index:           make(map[string]map[int]float64),
		idf:             make(map[string]float64),
		resultTagIdx:    make(map[string][]int),
		techniqueTagIdx: make(map[string][]int),
	}
	cs.build()
	return cs, nil
}

func (cs *TaggedCorpusSearcher) Len() int { return len(cs.queries) }

func (cs *TaggedCorpusSearcher) build() {
	n := len(cs.queries)
	if n == 0 {
		return
	}

	// Build tag indices
	for i, q := range cs.queries {
		for _, t := range q.ResultTags {
			cs.resultTagIdx[t] = append(cs.resultTagIdx[t], i)
		}
		for _, t := range q.TechniqueTags {
			cs.techniqueTagIdx[t] = append(cs.techniqueTagIdx[t], i)
		}
	}

	// Build TF-IDF index — search text is description + tags + code (weighted)
	tf := make([]map[string]float64, n)
	df := make(map[string]int)

	for i, q := range cs.queries {
		// Compose a weighted search document:
		// description x3 (most signal), tags x2, code x1
		searchDoc := strings.Join([]string{
			q.Description, q.Description, q.Description,
			strings.Join(q.ResultTags, " "), strings.Join(q.ResultTags, " "),
			strings.Join(q.TechniqueTags, " "), strings.Join(q.TechniqueTags, " "),
			q.Text,
		}, " ")

		terms := tokenizeText(searchDoc)
		counts := make(map[string]int, len(terms))
		for _, t := range terms {
			counts[t]++
		}
		tf[i] = make(map[string]float64, len(counts))
		for term, cnt := range counts {
			tf[i][term] = 1 + math.Log(float64(cnt))
			df[term]++
		}
	}

	for term, docFreq := range df {
		cs.idf[term] = math.Log(float64(1+n)/float64(1+docFreq)) + 1.0
	}

	for i, tfDoc := range tf {
		for term, tfVal := range tfDoc {
			weight := tfVal * cs.idf[term]
			if cs.index[term] == nil {
				cs.index[term] = make(map[int]float64)
			}
			cs.index[term][i] = weight
		}
	}
}

// Search finds the top-k tagged queries matching the query and optional tag filters.
// resultTag and techniqueTag, if non-empty, restrict results to entries with that tag.
func (cs *TaggedCorpusSearcher) Search(query, resultTag, techniqueTag string, topK int) []TaggedQueryResult {
	if len(cs.queries) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 5
	}

	// Compute candidate set from tag filters (intersection if both set)
	candidates := cs.candidateSet(resultTag, techniqueTag)

	// TF-IDF scoring over candidates (or all docs if no filter)
	queryTerms := tokenizeText(query)
	scores := make(map[int]float64)

	if len(queryTerms) > 0 {
		qCounts := make(map[string]int)
		for _, t := range queryTerms {
			qCounts[t]++
		}
		qVec := make(map[string]float64)
		for term, cnt := range qCounts {
			if idfVal, ok := cs.idf[term]; ok {
				qVec[term] = (1 + math.Log(float64(cnt))) * idfVal
			}
		}

		for term, qw := range qVec {
			if postings, ok := cs.index[term]; ok {
				for docIdx, dw := range postings {
					if candidates == nil || candidates[docIdx] {
						scores[docIdx] += qw * dw
					}
				}
			}
		}
	} else if candidates != nil {
		// No query text, just return all matching the tag filter with score=1
		for idx := range candidates {
			scores[idx] = 1.0
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
	results := make([]TaggedQueryResult, topK)
	for i, p := range pairs[:topK] {
		q := cs.queries[p.idx]
		results[i] = TaggedQueryResult{
			Text:          q.Text,
			File:          q.File,
			Description:   q.Description,
			ResultTags:    q.ResultTags,
			TechniqueTags: q.TechniqueTags,
			Score:         math.Round(p.score*1e4) / 1e4,
		}
	}
	return results
}

// AllResultTags returns a sorted list of all distinct result tags in the index.
func (cs *TaggedCorpusSearcher) AllResultTags() []string {
	tags := make([]string, 0, len(cs.resultTagIdx))
	for t := range cs.resultTagIdx {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// AllTechniqueTags returns a sorted list of all distinct technique tags in the index.
func (cs *TaggedCorpusSearcher) AllTechniqueTags() []string {
	tags := make([]string, 0, len(cs.techniqueTagIdx))
	for t := range cs.techniqueTagIdx {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// candidateSet returns the set of doc indices that match the tag filters.
// Returns nil if no filters are set (meaning all docs are candidates).
func (cs *TaggedCorpusSearcher) candidateSet(resultTag, techniqueTag string) map[int]bool {
	var rSet, tSet map[int]bool

	if resultTag != "" {
		indices := cs.resultTagIdx[resultTag]
		rSet = make(map[int]bool, len(indices))
		for _, i := range indices {
			rSet[i] = true
		}
	}
	if techniqueTag != "" {
		indices := cs.techniqueTagIdx[techniqueTag]
		tSet = make(map[int]bool, len(indices))
		for _, i := range indices {
			tSet[i] = true
		}
	}

	if rSet == nil && tSet == nil {
		return nil
	}
	if rSet == nil {
		return tSet
	}
	if tSet == nil {
		return rSet
	}
	// Intersection
	out := make(map[int]bool)
	for i := range rSet {
		if tSet[i] {
			out[i] = true
		}
	}
	return out
}

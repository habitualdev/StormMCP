package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	tokCLS = 101
	tokSEP = 102
	tokPAD = 0
	tokUNK = 100

	defaultMaxLen = 128
)

// BertTokenizer is a pure-Go BERT WordPiece tokenizer.
type BertTokenizer struct {
	vocab    map[string]int32
	maxLen   int
	unkToken string
}

type tokenizerFile struct {
	Truncation *struct {
		MaxLength int `json:"max_length"`
	} `json:"truncation"`
	Model struct {
		Vocab    map[string]int32 `json:"vocab"`
		UnkToken string           `json:"unk_token"`
	} `json:"model"`
}

func NewBertTokenizer(data []byte) (*BertTokenizer, error) {
	var tj tokenizerFile
	if err := json.Unmarshal(data, &tj); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}
	if len(tj.Model.Vocab) == 0 {
		return nil, fmt.Errorf("tokenizer.json: vocab is empty")
	}

	maxLen := defaultMaxLen
	if tj.Truncation != nil && tj.Truncation.MaxLength > 0 {
		maxLen = tj.Truncation.MaxLength
	}
	if maxLen > defaultMaxLen {
		maxLen = defaultMaxLen
	}

	unk := tj.Model.UnkToken
	if unk == "" {
		unk = "[UNK]"
	}

	return &BertTokenizer{vocab: tj.Model.Vocab, maxLen: maxLen, unkToken: unk}, nil
}

func (t *BertTokenizer) Encode(text string) ([]int64, []int64) {
	wordpieceIDs := t.tokenize(text)

	budget := t.maxLen - 2
	if len(wordpieceIDs) > budget {
		wordpieceIDs = wordpieceIDs[:budget]
	}

	ids := make([]int64, 0, t.maxLen)
	ids = append(ids, tokCLS)
	for _, id := range wordpieceIDs {
		ids = append(ids, int64(id))
	}
	ids = append(ids, tokSEP)

	mask := make([]int64, len(ids))
	for i := range mask {
		mask[i] = 1
	}

	for len(ids) < t.maxLen {
		ids = append(ids, tokPAD)
		mask = append(mask, 0)
	}

	return ids, mask
}

func (t *BertTokenizer) tokenize(text string) []int32 {
	text = bertNormalize(text)
	words := bertPreTokenize(text)
	var ids []int32
	for _, word := range words {
		ids = append(ids, t.wordpiece(word)...)
	}
	return ids
}

func bertNormalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0 || r == 0xFFFD {
			continue
		}
		if isControl(r) {
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
			continue
		}
		if isCJK(r) {
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	lower := strings.ToLower(b.String())
	nfd := norm.NFD.String(lower)
	var out strings.Builder
	out.Grow(len(nfd))
	for _, r := range nfd {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.IsControl(r)
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}

func bertPreTokenize(text string) []string {
	var words []string
	for _, part := range strings.Fields(text) {
		words = append(words, splitOnPunct(part)...)
	}
	return words
}

func splitOnPunct(word string) []string {
	var out []string
	start := 0
	hasPunct := false
	runes := []rune(word)
	for i, r := range runes {
		if isPunct(r) {
			if i > start {
				out = append(out, string(runes[start:i]))
			}
			out = append(out, string(r))
			start = i + 1
			hasPunct = true
		}
	}
	if !hasPunct {
		return []string{word}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}

func isPunct(r rune) bool {
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return true
	}
	cp := int(r)
	return (cp >= 33 && cp <= 47) ||
		(cp >= 58 && cp <= 64) ||
		(cp >= 91 && cp <= 96) ||
		(cp >= 123 && cp <= 126)
}

const maxInputCharsPerWord = 100

func (t *BertTokenizer) wordpiece(word string) []int32 {
	if len([]rune(word)) > maxInputCharsPerWord {
		if id, ok := t.vocab[t.unkToken]; ok {
			return []int32{id}
		}
		return []int32{tokUNK}
	}

	runes := []rune(word)
	var subTokenIDs []int32
	start := 0

	for start < len(runes) {
		end := len(runes)
		found := false
		for end > start {
			substr := string(runes[start:end])
			if start > 0 {
				substr = "##" + substr
			}
			if id, ok := t.vocab[substr]; ok {
				subTokenIDs = append(subTokenIDs, id)
				start = end
				found = true
				break
			}
			end--
		}
		if !found {
			if id, ok := t.vocab[t.unkToken]; ok {
				return []int32{id}
			}
			return []int32{tokUNK}
		}
	}

	return subTokenIDs
}

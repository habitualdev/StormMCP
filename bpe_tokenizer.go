package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ─── model constants ──────────────────────────────────────────────────────────

const (
	taskPrefix = "generate storm: "
	maxSrcLen  = 96
	bosID      = int32(1)
	eosID      = int32(2)
	padID      = int32(0)
	unkID      = int32(3)
	dModel     = 768
	vocabSize  = 32108
)

// ─── byte-level BPE tokenizer ─────────────────────────────────────────────────

type BPETokenizer struct {
	vocab      map[string]int32
	idToToken  []string
	mergeRanks map[string]int
	addedIDs   map[int32]bool
	byteToChar [256]rune
	charToByte map[rune]byte
}

func buildByteMaps() ([256]rune, map[rune]byte) {
	var b2c [256]rune
	c2b := make(map[rune]byte, 256)
	n := 0
	for b := 0; b < 256; b++ {
		pass := (b >= 33 && b <= 126) || (b >= 161 && b <= 172) || (b >= 174 && b <= 255)
		var ch rune
		if pass {
			ch = rune(b)
		} else {
			ch = rune(256 + n)
			n++
		}
		b2c[b] = ch
		c2b[ch] = byte(b)
	}
	return b2c, c2b
}

func loadTokenizerFromBytes(vocabJSON, mergesTxt, atJSON []byte) (*BPETokenizer, error) {
	var vocab map[string]int32
	if err := json.Unmarshal(vocabJSON, &vocab); err != nil {
		return nil, fmt.Errorf("parse vocab.json: %w", err)
	}

	mergeRanks := make(map[string]int)
	for rank, line := range strings.Split(string(mergesTxt), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		mergeRanks[line] = rank
	}

	maxID := int32(0)
	for _, id := range vocab {
		if id > maxID {
			maxID = id
		}
	}
	idToToken := make([]string, maxID+1)
	for tok, id := range vocab {
		idToToken[id] = tok
	}

	addedIDs := make(map[int32]bool)
	if len(atJSON) > 0 {
		var at map[string]int32
		if json.Unmarshal(atJSON, &at) == nil {
			for tok, id := range at {
				addedIDs[id] = true
				for int(id) >= len(idToToken) {
					idToToken = append(idToToken, "")
				}
				idToToken[id] = tok
			}
		}
	}

	b2c, c2b := buildByteMaps()
	return &BPETokenizer{
		vocab:      vocab,
		idToToken:  idToToken,
		mergeRanks: mergeRanks,
		addedIDs:   addedIDs,
		byteToChar: b2c,
		charToByte: c2b,
	}, nil
}

func (t *BPETokenizer) byteLevelEncode(s string) string {
	raw := []byte(s)
	out := make([]rune, len(raw))
	for i, b := range raw {
		out[i] = t.byteToChar[b]
	}
	return string(out)
}

func (t *BPETokenizer) bpeMerge(piece string) []string {
	encoded := t.byteLevelEncode(piece)
	runes := []rune(encoded)
	syms := make([]string, len(runes))
	for i, r := range runes {
		syms[i] = string(r)
	}

	for len(syms) > 1 {
		bestRank, bestPos := math.MaxInt32, -1
		for i := 0; i < len(syms)-1; i++ {
			key := syms[i] + " " + syms[i+1]
			if rank, ok := t.mergeRanks[key]; ok && rank < bestRank {
				bestRank, bestPos = rank, i
			}
		}
		if bestPos < 0 {
			break
		}
		merged := syms[bestPos] + syms[bestPos+1]
		next := make([]string, 0, len(syms)-1)
		next = append(next, syms[:bestPos]...)
		next = append(next, merged)
		next = append(next, syms[bestPos+2:]...)
		syms = next
	}
	return syms
}

func bpePreSplit(text string) []string {
	var pieces []string
	runes := []rune(text)
	n := len(runes)
	i := 0
	for i < n {
		start := i
		if runes[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		r := runes[i]
		switch {
		case bpeIsLetter(r):
			for i < n && bpeIsLetter(runes[i]) {
				i++
			}
		case bpeIsDigit(r):
			cnt := 0
			for i < n && bpeIsDigit(runes[i]) && cnt < 3 {
				i++
				cnt++
			}
		default:
			for i < n && runes[i] != ' ' && !bpeIsLetter(runes[i]) && !bpeIsDigit(runes[i]) {
				i++
			}
		}
		pieces = append(pieces, string(runes[start:i]))
	}
	return pieces
}

func bpeIsLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func bpeIsDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// Encode tokenizes text and returns [BOS, ...token ids..., EOS], capped at maxSrcLen.
func (t *BPETokenizer) Encode(text string) []int32 {
	ids := []int32{bosID}
	for _, piece := range bpePreSplit(text) {
		for _, sym := range t.bpeMerge(piece) {
			if id, ok := t.vocab[sym]; ok {
				ids = append(ids, id)
			} else {
				ids = append(ids, unkID)
			}
		}
	}
	ids = append(ids, eosID)
	if len(ids) > maxSrcLen {
		ids = ids[:maxSrcLen-1]
		ids = append(ids, eosID)
	}
	return ids
}

// sortedKeys returns the keys of a string->int32 map sorted by descending string length.
func sortedKeys(m map[string]int32) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}

// Decode converts a slice of token IDs to a UTF-8 string.
func (t *BPETokenizer) Decode(ids []int32) string {
	var buf []byte
	for _, id := range ids {
		if id <= 4 || (id >= 32000 && id <= 32099) {
			continue
		}
		if int(id) >= len(t.idToToken) {
			continue
		}
		tok := t.idToToken[id]
		if tok == "" {
			continue
		}
		if t.addedIDs[id] {
			buf = append(buf, []byte(tok)...)
			continue
		}
		for _, r := range []rune(tok) {
			if b, ok := t.charToByte[r]; ok {
				buf = append(buf, b)
			}
		}
	}
	return string(buf)
}

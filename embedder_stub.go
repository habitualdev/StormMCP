//go:build noort

package main

import "fmt"

type Embedder struct {
	tokenizer *BertTokenizer
	dim       int
}

func NewEmbedder(_, _, tokenizerData []byte) (*Embedder, error) {
	tok, err := NewBertTokenizer(tokenizerData)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: %w", err)
	}
	return &Embedder{tokenizer: tok, dim: 384}, nil
}

func (e *Embedder) Embed(_ string) ([]float32, error) {
	return nil, fmt.Errorf("ONNX inference not available (built with -tags noort)")
}

func (e *Embedder) Dim() int { return e.dim }
func (e *Embedder) Close()   {}

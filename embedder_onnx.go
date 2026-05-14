//go:build !noort

package main

import (
	"fmt"
	"os"
	"path/filepath"

	ort "github.com/yalue/onnxruntime_go"
)

// Embedder runs all-MiniLM-L6-v2 inference via ONNX Runtime.
type Embedder struct {
	session   *ort.DynamicAdvancedSession
	tokenizer *BertTokenizer
	dim       int
}

// NewEmbedder creates the embedding session.  The ORT environment must already
// be initialised via initORTEnv before calling this.
func NewEmbedder(_, onnxModelBytes, tokenizerData []byte) (*Embedder, error) {
	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		onnxModelBytes,
		[]string{"input_ids", "attention_mask"},
		[]string{"embeddings"},
		nil,
	)
	if err != nil {
		modelPath, ferr := extractTempFile(onnxModelBytes, "storm-embed-*.onnx")
		if ferr != nil {
			return nil, fmt.Errorf("ORT session (data): %w; extract fallback: %w", err, ferr)
		}
		session, err = ort.NewDynamicAdvancedSession(
			modelPath,
			[]string{"input_ids", "attention_mask"},
			[]string{"embeddings"},
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("ORT session (file): %w", err)
		}
	}

	tok, err := NewBertTokenizer(tokenizerData)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: %w", err)
	}

	return &Embedder{session: session, tokenizer: tok, dim: 384}, nil
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	inputIDs, attMask := e.tokenizer.Encode(text)
	seqLen := int64(len(inputIDs))
	shape := ort.NewShape(1, seqLen)

	idsTensor, err := ort.NewTensor(shape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer idsTensor.Destroy()

	maskTensor, err := ort.NewTensor(shape, attMask)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer maskTensor.Destroy()

	outTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(e.dim)))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outTensor.Destroy()

	err = e.session.Run(
		[]ort.ArbitraryTensor{idsTensor, maskTensor},
		[]ort.ArbitraryTensor{outTensor},
	)
	if err != nil {
		return nil, fmt.Errorf("ORT Run: %w", err)
	}

	src := outTensor.GetData()
	result := make([]float32, len(src))
	copy(result, src)
	return result, nil
}

func (e *Embedder) Dim() int { return e.dim }

func (e *Embedder) Close() {
	if e.session != nil {
		e.session.Destroy()
	}
}

func extractTempFile(data []byte, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return filepath.Clean(f.Name()), nil
}

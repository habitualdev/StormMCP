//go:build !noort

package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"

	ort "github.com/yalue/onnxruntime_go"
)

// StormInfer runs CodeT5-base ONNX inference to generate Storm queries.
// The ORT environment must be initialised via initORTEnv before calling NewStormInfer.
type StormInfer struct {
	tok     *BPETokenizer
	encoder *ort.DynamicAdvancedSession
	decoder *ort.DynamicAdvancedSession
	cleanup func()
}

// NewStormInfer loads the CodeT5 encoder/decoder sessions from the embedded
// gzip blobs (encoderGZ / decoderGZ).  Returns nil if no embedded blobs are
// present (built without -tags embedquery), which callers must handle.
func NewStormInfer() (*StormInfer, error) {
	if len(encoderGZ) == 0 {
		return nil, nil
	}

	encPath, cleanEnc, err := decompressModelToTemp(encoderGZ, "storm-enc-*.onnx")
	if err != nil {
		return nil, fmt.Errorf("decompress encoder: %w", err)
	}

	decPath, cleanDec, err := decompressModelToTemp(decoderGZ, "storm-dec-*.onnx")
	if err != nil {
		cleanEnc()
		return nil, fmt.Errorf("decompress decoder: %w", err)
	}

	vocabJSON, err := gunzipBytes(vocabGZ)
	if err != nil {
		cleanEnc(); cleanDec()
		return nil, fmt.Errorf("decompress vocab: %w", err)
	}
	mergesTxt, err := gunzipBytes(mergesGZ)
	if err != nil {
		cleanEnc(); cleanDec()
		return nil, fmt.Errorf("decompress merges: %w", err)
	}

	tok, err := loadTokenizerFromBytes(vocabJSON, mergesTxt, addedTokensJSON)
	if err != nil {
		cleanEnc(); cleanDec()
		return nil, fmt.Errorf("tokenizer: %w", err)
	}

	encoder, err := ort.NewDynamicAdvancedSession(
		encPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"last_hidden_state"},
		nil,
	)
	if err != nil {
		cleanEnc(); cleanDec()
		return nil, fmt.Errorf("encoder session: %w", err)
	}

	// Request only "logits" — ORT skips computing the 96 KV-cache outputs.
	decoder, err := ort.NewDynamicAdvancedSession(
		decPath,
		[]string{"encoder_attention_mask", "input_ids", "encoder_hidden_states"},
		[]string{"logits"},
		nil,
	)
	if err != nil {
		encoder.Destroy()
		cleanEnc(); cleanDec()
		return nil, fmt.Errorf("decoder session: %w", err)
	}

	return &StormInfer{
		tok:     tok,
		encoder: encoder,
		decoder: decoder,
		cleanup: func() { cleanEnc(); cleanDec() },
	}, nil
}

func (si *StormInfer) Close() {
	if si.encoder != nil {
		si.encoder.Destroy()
	}
	if si.decoder != nil {
		si.decoder.Destroy()
	}
	si.cleanup()
}

// Generate runs greedy inference and returns the predicted Storm query.
func (si *StormInfer) Generate(question string, maxTgtLen int) (string, error) {
	srcIDs := si.tok.Encode(taskPrefix + question)
	encLen := int64(len(srcIDs))

	srcIDs64 := make([]int64, len(srcIDs))
	mask64 := make([]int64, len(srcIDs))
	for i, id := range srcIDs {
		srcIDs64[i] = int64(id)
		mask64[i] = 1
	}

	inputIDsTensor, err := ort.NewTensor(ort.NewShape(1, encLen), srcIDs64)
	if err != nil {
		return "", fmt.Errorf("encoder input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Destroy()

	attMaskTensor, err := ort.NewTensor(ort.NewShape(1, encLen), mask64)
	if err != nil {
		return "", fmt.Errorf("encoder attention_mask tensor: %w", err)
	}
	defer attMaskTensor.Destroy()

	encOutTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, encLen, dModel))
	if err != nil {
		return "", fmt.Errorf("encoder output tensor: %w", err)
	}
	defer encOutTensor.Destroy()

	if err := si.encoder.Run(
		[]ort.ArbitraryTensor{inputIDsTensor, attMaskTensor},
		[]ort.ArbitraryTensor{encOutTensor},
	); err != nil {
		return "", fmt.Errorf("encoder run: %w", err)
	}

	encHidden := encOutTensor.GetData()

	decIDs := []int64{int64(padID)}
	var generatedIDs []int32

	for step := 0; step < maxTgtLen; step++ {
		decLen := int64(len(decIDs))

		decInputTensor, err := ort.NewTensor(ort.NewShape(1, decLen), decIDs)
		if err != nil {
			return "", fmt.Errorf("step %d decoder input tensor: %w", step, err)
		}
		encMaskTensor, err := ort.NewTensor(ort.NewShape(1, encLen), mask64)
		if err != nil {
			decInputTensor.Destroy()
			return "", fmt.Errorf("step %d enc_mask tensor: %w", step, err)
		}
		encHiddenTensor, err := ort.NewTensor(ort.NewShape(1, encLen, dModel), encHidden)
		if err != nil {
			decInputTensor.Destroy()
			encMaskTensor.Destroy()
			return "", fmt.Errorf("step %d enc_hidden tensor: %w", step, err)
		}
		logitsTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, decLen, int64(vocabSize)))
		if err != nil {
			decInputTensor.Destroy()
			encMaskTensor.Destroy()
			encHiddenTensor.Destroy()
			return "", fmt.Errorf("step %d logits tensor: %w", step, err)
		}

		runErr := si.decoder.Run(
			[]ort.ArbitraryTensor{encMaskTensor, decInputTensor, encHiddenTensor},
			[]ort.ArbitraryTensor{logitsTensor},
		)
		decInputTensor.Destroy()
		encMaskTensor.Destroy()
		encHiddenTensor.Destroy()

		if runErr != nil {
			logitsTensor.Destroy()
			return "", fmt.Errorf("step %d decoder run: %w", step, runErr)
		}

		logits := logitsTensor.GetData()
		logitsTensor.Destroy()

		offset := int(decLen-1) * vocabSize
		lastLogits := logits[offset : offset+vocabSize]

		bestID, bestVal := 0, float32(-math.MaxFloat32)
		for i, v := range lastLogits {
			if v > bestVal {
				bestVal, bestID = v, i
			}
		}

		nextID := int32(bestID)
		if nextID == eosID {
			break
		}
		generatedIDs = append(generatedIDs, nextID)
		decIDs = append(decIDs, int64(nextID))
	}

	return si.tok.Decode(generatedIDs), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func decompressModelToTemp(gz []byte, pattern string) (path string, cleanup func(), err error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return "", nil, err
	}
	defer r.Close()

	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	path = f.Name()
	return path, func() { os.Remove(path) }, nil
}

func gunzipBytes(gz []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

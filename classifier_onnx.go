//go:build !noort

package main

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

// Classifier runs the LightGBM binary classifier (binary_nozip.onnx).
// Input:  float_input  [batch, 31]  float32
// Output: label        [batch]      int64
//         probabilities [batch, 2]  float32
type Classifier struct {
	session *ort.DynamicAdvancedSession
}

func NewClassifier(onnxModelBytes []byte) (*Classifier, error) {
	// ORT environment must already be initialised by NewEmbedder.
	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		onnxModelBytes,
		[]string{"float_input"},
		[]string{"label", "probabilities"},
		nil,
	)
	if err != nil {
		modelPath, ferr := extractTempFile(onnxModelBytes, "storm-clf-*.onnx")
		if ferr != nil {
			return nil, fmt.Errorf("classifier ORT session (data): %w; extract: %w", err, ferr)
		}
		session, err = ort.NewDynamicAdvancedSession(
			modelPath,
			[]string{"float_input"},
			[]string{"label", "probabilities"},
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("classifier ORT session (file): %w", err)
		}
	}
	return &Classifier{session: session}, nil
}

// Predict runs inference on a single feature vector (length 31).
// Returns (isViolation, pViolation).
func (c *Classifier) Predict(feats []float32) (bool, float32, error) {
	if len(feats) != nFeatures {
		return false, 0, fmt.Errorf("expected %d features, got %d", nFeatures, len(feats))
	}

	inTensor, err := ort.NewTensor(ort.NewShape(1, int64(nFeatures)), feats)
	if err != nil {
		return false, 0, fmt.Errorf("create input tensor: %w", err)
	}
	defer inTensor.Destroy()

	labelTensor, err := ort.NewEmptyTensor[int64](ort.NewShape(1))
	if err != nil {
		return false, 0, fmt.Errorf("create label tensor: %w", err)
	}
	defer labelTensor.Destroy()

	probTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 2))
	if err != nil {
		return false, 0, fmt.Errorf("create prob tensor: %w", err)
	}
	defer probTensor.Destroy()

	err = c.session.Run(
		[]ort.ArbitraryTensor{inTensor},
		[]ort.ArbitraryTensor{labelTensor, probTensor},
	)
	if err != nil {
		return false, 0, fmt.Errorf("ORT Run: %w", err)
	}

	labels := labelTensor.GetData()
	probs := probTensor.GetData()

	isViol := len(labels) > 0 && labels[0] == 1
	var pViol float32
	if len(probs) >= 2 {
		pViol = probs[1]
	}
	return isViol, pViol, nil
}

func (c *Classifier) Close() {
	if c.session != nil {
		c.session.Destroy()
	}
}

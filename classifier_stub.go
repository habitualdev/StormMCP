//go:build noort

package main

import "fmt"

type Classifier struct{}

func NewClassifier(_ []byte) (*Classifier, error) {
	return &Classifier{}, nil
}

func (c *Classifier) Predict(_ []float32) (bool, float32, error) {
	return false, 0, fmt.Errorf("ONNX classifier not available (built with -tags noort)")
}

func (c *Classifier) Close() {}

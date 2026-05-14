//go:build noort

package main

import "fmt"

// StormInfer is a no-op stub when built with -tags noort.
type StormInfer struct{}

func NewStormInfer() (*StormInfer, error) { return nil, nil }

func (si *StormInfer) Generate(_ string, _ int) (string, error) {
	return "", fmt.Errorf("ORT not available (built with -tags noort)")
}

func (si *StormInfer) Close() {}

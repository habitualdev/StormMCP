//go:build !noort

package main

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

// initORTEnv extracts the ORT shared library (if embedded) and initialises
// the ORT environment. Must be called exactly once before any ORT session is
// created.  Pair with a deferred destroyORTEnv call in main.
func initORTEnv(libBytes []byte) error {
	if len(libBytes) > 0 {
		path, err := extractTempFile(libBytes, "onnxruntime-*"+ortLibExt)
		if err != nil {
			return fmt.Errorf("extract ORT lib: %w", err)
		}
		ort.SetSharedLibraryPath(path)
	}
	return ort.InitializeEnvironment()
}

func destroyORTEnv() {
	ort.DestroyEnvironment()
}

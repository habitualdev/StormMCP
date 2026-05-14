//go:build (!linux && !darwin && !windows) || noort

package main

// ortLibBytes is empty on unsupported platforms or when built with -tags noort.
// The ORT shared library must be on the system library path in that case.
var ortLibBytes []byte

const ortLibExt = ".so"

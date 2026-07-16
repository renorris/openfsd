//go:build stress

package server

import "testing"

// TestStressTagPresent is compiled only with -tags=stress so CI can optionally
// run a dedicated stress job that asserts the build tag path exists.
func TestStressTagPresent(t *testing.T) {
	t.Log("stress build tag active")
}

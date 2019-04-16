// Package rawvm implements features for Istio mesh expansion.
package rawvm

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/resource"
)

// gceComponent is the implementation for GCE VM.
type gceComponent struct {
}

// NewGCE creates a GCE instance.
func NewGCE(ctx resource.Context, config Config) (Instance, error) {
	return nil, fmt.Errorf("NewGCE not implemented")
}

// Execute implements Instance.Execute interface.
func (c *gceComponent) Execute(cmds []string) (string, error) {
	return "", fmt.Errorf("GCE, Execute not implemented yet")
}

// Close implements Instance.Close interface.
func (c *gceComponent) Close() (err error) {
	return nil
}

// Package rawvm provides component for Istio mesh expansion.
package rawvm

import (
	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/resource"
	old_framework "istio.io/istio/tests/e2e/framework"
)

// Instance represents a VM instance running the workload via Istio
// mesh expansion.
type Instance interface {
	// Execute executes a command in the VM instance, returns the output and the error.
	Execute(command string) (string, error)

	// Close is invoked when VM instance is cleaned up.
	Close() error
}

// Type for the VM workload.
type Type string

const (
	// Native type VM Type, not implemented yet.
	Native Type = "native"
	// GCE type VM Type.
	GCE Type = "gce"
)

// Config for VM instance.
type Config struct {
	Type        Type
	GCPVMConfig old_framework.GCPVMOpts
}

// New returns a VMInstance.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	// Currently only implements GCE VM.
	if cfg.Type != GCE {
		return nil, fmt.Errorf("only GCE typed VM are supported")
	}
	return NewGCE(ctx, cfg)
}

// Register reigsters a VM service by creating necessary Kubernetes resources.
// Speicifically, a Kubernetes service and ServiceEntry.
func Register(serviceName string, portList model.PortList) error {
	return nil
}

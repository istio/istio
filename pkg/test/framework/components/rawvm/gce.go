// Package rawvm implements features for Istio mesh expansion.
package rawvm

import (
	"fmt"

	old_framework "istio.io/istio/tests/e2e/framework"

	"istio.io/istio/pkg/test/framework/resource"
)

// gceComponent is the mesh expansion VM implementation on GCP.
type gceComponent struct {
	// id is the GCE instance id.
	id    resource.ID
	rawVM *old_framework.GCPRawVM
}

// NewGCE creates a GCE instance that finishes the setup with specified application.
func NewGCE(ctx resource.Context, config Config) (Instance, error) {
	c := &gceComponent{}
	if err := c.setup(config); err != nil {
		return nil, err
	}
	c.id = ctx.TrackResource(c)
	return c, nil
}

// setup is responsible for necessary setup work on the GCE instance.
func (c *gceComponent) setup(cfg Config) error {
	vm, err := old_framework.NewGCPRawVM(cfg.GCPVMConfig)
	if err != nil {
		return err
	}
	if err := vm.Setup(); err != nil {
		return fmt.Errorf("failed in gce instance setup stage. %v", err)
	}
	c.rawVM = vm
	return nil
}

// Execute implements Instance.Execute interface.
func (c *gceComponent) Execute(cmd string) (string, error) {
	return c.rawVM.SecureShell(cmd)
}

// Close implements Instance.Close interface.
func (c *gceComponent) Close() (err error) {
	if err := c.rawVM.Teardown(); err != nil {
		return err
	}
	return nil
}

func (c *gceComponent) ID() resource.ID {
	return c.id
}

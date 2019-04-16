// Package rawvm implements features for Istio mesh expansion.
package rawvm

import (
	"fmt"

	old_framework "istio.io/istio/tests/e2e/framework"

	"istio.io/istio/pkg/test/framework/resource"
)

// gceComponent is the implementation for GCE VM.
type gceComponent struct {
	// id is the GCE instance id.
	id resource.ID
	// masonFile is the mason client config file when running on the prow.
	// If specified, test claims the GCE instance by parsing mason file.
	masonFile string
}

// NewGCE creates a GCE instance that finishes the setup with specified application.
func NewGCE(ctx resource.Context, config Config) (Instance, error) {
	c := &gceComponent{}
	if err := c.setup(); err != nil {
		return nil, err
	}
	c.id = ctx.TrackResource(c)
	return c, nil
}

// setup is responsible for necessary setup work on the GCE instance. This includes
// - Download Kubernetes key cert;
// - Setup ingress Gateway IP in /etc/hots;
// - Start the Istio sidecar and node agent daemon processes;
// TODO(inclfy): implement it.
func (c *gceComponent) setup() error {
	if c.masonFile == "" {
		return fmt.Errorf("need to create GCE on the fly, not implemented")
	}
	_, err := old_framework.ParseGCEInstance("mason_file_from_flag")
	if err != nil {
		return err
	}
	// TODO: implement steps below.
	// - Propogate the info from GCERawVM to gceComponent from returned result.
	// - Setup the DNS
	return nil
}

// Execute implements Instance.Execute interface.
func (c *gceComponent) Execute(cmds []string) (string, error) {
	return "", fmt.Errorf("GCE, Execute not implemented yet")
}

// Close implements Instance.Close interface.
func (c *gceComponent) Close() (err error) {
	return nil
}

func (c *gceComponent) ID() resource.ID {
	return c.id
}

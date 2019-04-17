// Package rawvm implements features for Istio mesh expansion.
package rawvm

import (
	"fmt"

	old_framework "istio.io/istio/tests/e2e/framework"

	"istio.io/istio/pkg/test/framework/resource"
)

// var (
// 	masonFile = flag.String("mason_info", "",
// 		"File created by Mason Client that provides information about the SUT")
// )

// gceComponent is the implementation for GCE VM.
type gceComponent struct {
	// id is the GCE instance id.
	id resource.ID
	// TODO: inline the struct once we delete old test.
	rawVM *old_framework.GCPRawVM
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
	vm, err := old_framework.NewGCPRawVM("default")
	// if *masonFile == "" {
	// 	return fmt.Errorf("need to create GCE on the fly, not implemented")
	// }
	// vm, err := old_framework.ParseGCEInstance(*masonFile)
	if err != nil {
		return err
	}
	if err := vm.Setup(); err != nil {
		return fmt.Errorf("failed in gce instance setup stage. %v", err)
	}
	c.rawVM = vm
	// fmt.Println("jianfeih debug, start to send hello world ssh command")
	// output, err := c.rawVM.SecureShell("echo hello && cat /etc/hosts")
	// if err != nil {
	// 	return err
	// }
	// fmt.Printf("jianfeih debug, the output is %v\n", output)
	// TODO: implement steps below.
	// - Setup the DNS
	return nil
}

// Execute implements Instance.Execute interface.
func (c *gceComponent) Execute(cmds []string) (string, error) {
	return "", fmt.Errorf("GCE, Execute not implemented yet")
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

// Package meshexp contains test suite for mesh expansion.
package meshexp

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/rawvm"
)

var (
	vmInstance rawvm.Instance
	// ports is the port the VM service exposes.
	ports = model.PortList{
		&model.Port{
			Name:     "http",
			Port:     8080,
			Protocol: model.ProtocolHTTP,
		},
	}
)

const (
	serviceName = ""
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("meshexp_test", m).
		// Restrict the test to the K8s environment only, tests will be skipped in native environment.
		RequireEnvironment(environment.Kube).
		// Deploy Istio on the cluster
		Setup(istio.SetupOnKube(nil, setupMeshExpansionInstall)).
		// Create a VM instance before running the test.
		Setup(setupVMInstance).
		Run()
}

func setupMeshExpansionInstall(cfg *istio.Config) {
	cfg.Values["global.meshExpansion.enabled"] = "true"
}

// setupVMInstance runs necessary setup on the VM instance and create service
// entry for VM application.
func setupVMInstance(ctx framework.SuiteContext) error {
	var err error
	vmInstance, err = rawvm.New(ctx, rawvm.Config{
		Type: rawvm.GCE,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM service %v", err)
	}
	rawvm.Register(serviceName, ports)
	// TODO: setup a app in Kubernetes cluster to send request to VM app.
	return nil
}

// TestKubernetesToVM sends a request to a pod in Kubernetes cluster, then the pod sends the request
// to app runs on the VM, returns success if VM app returns success result.
func TestKubernetesToVM(t *testing.T) {
}

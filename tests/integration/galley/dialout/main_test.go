package dialout

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mcpserver"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var (
	// List all the collections required for running tests in this package
	mcpCollections = []string{
		"istio/networking/v1alpha3/gateways",
	}
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("galley_with_dialout_test", m).
		Setup(setupMCPSinkServer).
		SetupOnEnv(environment.Kube, istio.Setup(nil, nil)).
		SetupOnEnv(environment.Kube, updateGalleyDeploymentConfigWithSinkAddress).
		Run()
}

// setupMCPSinkServer sets up the MCP sink server.
func setupMCPSinkServer(ctx resource.Context) error {
	sinkConfig := mcpserver.SinkConfig{Collections: mcpCollections}
	mcpInstance, err := mcpserver.NewSink(ctx, sinkConfig)
	if err != nil {
		scopes.CI.Errorf("cannot setup mcp-sinkserver: %v", err)
		return err
	}
	ctx.Settings().MiscSettings["mcp-instance"] = mcpInstance
	scopes.Framework.Infof("MCP sink server running at %s", mcpInstance.Address())
	return nil
}

// updateGalleyDeploymentConfigWithSinkAddress brings down the Galley deployment in Kubernetes
// environment and adds sinkAddress to the list of command-line parameters to enable callout in
// Galley and redeploys it in the same environment. Ideally we should be able to pass Galley
// arguments while setting up environment. Currently, it is not possible and hence we have this hack.
func updateGalleyDeploymentConfigWithSinkAddress(ctx resource.Context) error {
	c, err := istio.DefaultConfig(ctx)
	if err != nil {
		return err
	}
	ns := c.ConfigNamespace
	kubeEnv := ctx.Environment().(*kube.Environment)
	galleyDeploymentName := "istio-galley"
	galleyDeploy, err := kubeEnv.GetDeployment(ns, galleyDeploymentName)
	if err != nil {
		return err
	}
	mcpInstance, err := mcpserver.GetSinkHandle(ctx)
	if err != nil {
		return err
	}

	// Now patch the configuration - for galley startup command add sinkAuth=NONE
	// and --sinkAddress=<address> command-line arguments. No sanitization of passed
	// address is required as it is only for testing purposes and it is completely
	// in our control
	for i := range galleyDeploy.Spec.Template.Spec.Containers {
		c := &galleyDeploy.Spec.Template.Spec.Containers[i]
		if c.Name == "galley" {
			c.Command = append(c.Command,
				"--sinkAuthMode=NONE",
				fmt.Sprintf("--sinkAddress=%s", mcpInstance.Address()))
			break
		}
	}

	// Now deploy galley again with the patched configuration
	_, err = kubeEnv.UpdateDeployment(ns, galleyDeploy)
	if err != nil {
		return err
	}
	if err = kubeEnv.WaitUntilDeploymentIsReady(ns, galleyDeploy.Name); err != nil {
		return err
	}
	return nil
}

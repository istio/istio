package istio

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var (
	mcSamples             = path.Join(env.IstioSrc, "samples", "multicluster")
	exposeIstiodGateway   = path.Join(mcSamples, "expose-istiod.yaml")
	exposeServicesGateway = path.Join(mcSamples, "expose-services.yaml")
	genGatewayScript      = path.Join(mcSamples, "gen-eastwest-gateway.sh")
)

// deployEastWestGateway will create a separate gateway deployment for cross-cluster discovery or cross-network services.
func (i *operatorComponent) deployEastWestGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Deploying east-west-gateway in ", cluster.Name())
	// generate k8s resources for the gateway
	cmd := exec.Command(genGatewayScript,
		"--istioNamespace", i.settings.SystemNamespace,
		"--manifests", filepath.Join(env.IstioSrc, "manifests"))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		"CLUSTER="+cluster.Name(),
		"NETWORK="+cluster.NetworkName())
	gwYaml, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed generating east-west gateway manifest for %s: %v", cluster.Name(), err)
	}
	i.saveManifestForCleanup(cluster.Name(), string(gwYaml))
	// push them to the cluster
	if err := i.ctx.Config(cluster).ApplyYAML(i.settings.IngressNamespace, string(gwYaml)); err != nil {
		return fmt.Errorf("failed applying east-west gateway deployment to %s: %v", cluster.Name(), err)
	}
	return nil
}

func (i *operatorComponent) applyCrossNetworkGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Exposing services via east-west-gateway in ", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeServicesGateway)
}

func (i *operatorComponent) applyIstiodGateway(cluster resource.Cluster) error {
	scopes.Framework.Infof("Exposing istiod via east-west-gateway in ", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeIstiodGateway)
}

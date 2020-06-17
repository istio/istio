// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package istio

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/hashicorp/go-multierror"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshAPI "istio.io/api/mesh/v1alpha1"
	pkgAPI "istio.io/istio/operator/pkg/apis/istio/v1alpha1"

	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/test/cert/ca"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/protomarshal"
)

// TODO: dynamically generate meshID to support multi-tenancy tests
const meshID = "testmesh0"

type operatorComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment

	mu sync.Mutex
	// installManifest includes the yamls use to install Istio. These can be deleted on cleanup
	// The key is the cluster name
	installManifest map[string]string
}

var _ io.Closer = &operatorComponent{}
var _ Instance = &operatorComponent{}
var _ resource.Dumper = &operatorComponent{}

// ID implements resource.Instance
func (i *operatorComponent) ID() resource.ID {
	return i.id
}

func (i *operatorComponent) Settings() Config {
	return i.settings
}

// When we cleanup, we should not delete CRDs. This will filter out all the crds
func removeCRDs(istioYaml string) string {
	allParts := yml.SplitString(istioYaml)
	nonCrds := make([]string, 0, len(allParts))

	// Make the regular expression multi-line and anchor to the beginning of the line.
	r := regexp.MustCompile(`(?m)^kind: CustomResourceDefinition$`)

	for _, p := range allParts {
		if r.Match([]byte(p)) {
			continue
		}
		nonCrds = append(nonCrds, p)
	}

	return yml.JoinString(nonCrds...)
}

var leaderElectionConfigMaps = []string{
	leaderelection.IngressController,
	leaderelection.NamespaceController,
	leaderelection.ValidationController,
}

func (i *operatorComponent) Close() (err error) {
	scopes.Framework.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	defer scopes.Framework.Infof("=== DONE: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	if i.settings.DeployIstio {
		for _, cluster := range i.environment.KubeClusters {
			if e := cluster.DeleteContents("", removeCRDs(i.installManifest[cluster.Name()])); e != nil {
				err = multierror.Append(err, e)
			}
			// Clean up dynamic leader election locks. This allows new test suites to become the leader without waiting 30s
			for _, cm := range leaderElectionConfigMaps {
				if e := cluster.CoreV1().ConfigMaps(i.settings.SystemNamespace).Delete(context.TODO(), cm,
					kubeApiMeta.DeleteOptions{}); e != nil {
					err = multierror.Append(err, e)
				}
			}
			if i.environment.IsMulticluster() {
				if e := cluster.DeleteNamespace(i.settings.SystemNamespace); e != nil {
					err = multierror.Append(err, e)
				}
				if e := cluster.WaitForNamespaceDeletion(i.settings.SystemNamespace, retry.Timeout(time.Minute)); e != nil {
					err = multierror.Append(err, e)
				}
			}
		}
	}
	return
}

func (i *operatorComponent) Dump() {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State...")

	for _, cluster := range i.environment.KubeClusters {
		d, err := i.ctx.CreateTmpDirectory(fmt.Sprintf("istio-state-%s", cluster.Name()))
		if err != nil {
			scopes.Framework.Errorf("Unable to create directory for dumping Istio contents: %v", err)
			return
		}
		kube2.DumpPods(cluster, d, i.settings.SystemNamespace)
	}
}

func (i *operatorComponent) saveInstallManifest(name string, out string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.installManifest[name] = out
}

func deploy(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	scopes.Framework.Infof("=== Istio Component Config ===")
	scopes.Framework.Infof("\n%s", cfg.String())
	scopes.Framework.Infof("================================")

	i := &operatorComponent{
		environment:     env,
		settings:        cfg,
		ctx:             ctx,
		installManifest: map[string]string{},
	}
	i.id = ctx.TrackResource(i)

	if !cfg.DeployIstio {
		scopes.Framework.Info("skipping deployment as specified in the config")
		return i, nil
	}

	// Top-level work dir for Istio deployment.
	workDir, err := ctx.CreateTmpDirectory("istio-deployment")
	if err != nil {
		return nil, err
	}

	// For multicluster, create and push the CA certs to all clusters to establish a shared root of trust.
	if env.IsMulticluster() {
		if err := deployCACerts(workDir, env, cfg); err != nil {
			return nil, err
		}
	}

	// Generate the istioctl config file
	iopFile := filepath.Join(workDir, "iop.yaml")
	if err := initIOPFile(cfg, env, iopFile, cfg.ControlPlaneValues); err != nil {
		return nil, err
	}

	remoteIopFile := iopFile
	if cfg.RemoteClusterValues != "" {
		remoteIopFile = filepath.Join(workDir, "remote.yaml")
		if err := initIOPFile(cfg, env, remoteIopFile, cfg.RemoteClusterValues); err != nil {
			return nil, err
		}
	}

	// Deploy the Istio control plane(s)
	errG := multierror.Group{}
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster) {
			cluster := cluster
			errG.Go(func() error {
				if err := deployControlPlane(i, cfg, cluster, iopFile); err != nil {
					return fmt.Errorf("failed deploying control plane to cluster %d: %v", cluster.Index(), err)
				}
				return nil
			})
		}
	}
	if errs := errG.Wait(); errs != nil {
		return nil, fmt.Errorf("%d errors occurred deploying control plane clusters: %v", errs.Len(), errs.ErrorOrNil())
	}

	// Wait for all of the control planes to be started before deploying remote clusters
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster) {
			if err := waitForControlPlane(i, cluster, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Deploy Istio to remote clusters
	errG = multierror.Group{}
	for _, cluster := range env.KubeClusters {
		if !env.IsControlPlaneCluster(cluster) {
			cluster := cluster
			errG.Go(func() error {
				if err := deployControlPlane(i, cfg, cluster, remoteIopFile); err != nil {
					return fmt.Errorf("failed deploying control plane to cluster %d: %v", cluster.Index(), err)
				}
				return nil
			})
		}
	}
	if errs := errG.Wait(); errs != nil {
		return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
	}

	if env.IsMulticluster() {
		// For multicluster, configure direct access so each control plane can get endpoints from all
		// API servers.
		if err := configureDirectAPIServerAccess(ctx, env, cfg); err != nil {
			return nil, err
		}
	}

	if env.IsMultinetwork() {
		// enable cross network traffic
		for _, cluster := range env.KubeClusters {
			if err := createCrossNetworkGateway(cluster, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Wait for all of the control planes to be started.
	for _, cluster := range env.KubeClusters {
		if err := waitForControlPlane(i, cluster, cfg); err != nil {
			return nil, err
		}
	}

	return i, nil
}

func initIOPFile(cfg Config, env *kube.Environment, iopFile string, valuesYaml string) error {
	operatorYaml := cfg.IstioOperatorConfigYAML(valuesYaml)

	operatorCfg := &pkgAPI.IstioOperator{}
	if err := protomarshal.ApplyYAML(operatorYaml, operatorCfg); err != nil {
		return fmt.Errorf("failed to unmsarshal base iop: %v", err)
	}
	var values = &pkgAPI.Values{}
	if operatorCfg.Spec.Values != nil {
		valuesYml, err := yaml.Marshal(operatorCfg.Spec.Values)
		if err != nil {
			return fmt.Errorf("failed to marshal base values: %v", err)
		}
		if err := protomarshal.ApplyYAML(string(valuesYml), values); err != nil {
			return fmt.Errorf("failed to unmsarshal base values: %v", err)
		}
	}

	if env.IsMultinetwork() {
		if values.Global == nil {
			values.Global = &pkgAPI.GlobalConfig{}
		}
		if values.Global.MeshNetworks == nil {
			meshNetworks, err := protomarshal.ToJSONMap(meshNetworkSettings(cfg, env))
			if err != nil {
				return fmt.Errorf("failed to convert meshNetworks: %v", err)
			}
			values.Global.MeshNetworks = meshNetworks["networks"].(map[string]interface{})
		}
	}

	valuesMap, err := protomarshal.ToJSONMap(values)
	if err != nil {
		return fmt.Errorf("failed to convert values to json map: %v", err)
	}
	operatorCfg.Spec.Values = valuesMap

	// marshaling entire operatorCfg causes panic because of *time.Time in ObjectMeta
	out, err := protomarshal.ToYAML(operatorCfg.Spec)
	if err != nil {
		return fmt.Errorf("failed marshaling iop spec: %v", err)
	}

	out = fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
%s`, Indent(out, "  "))

	if err := ioutil.WriteFile(iopFile, []byte(out), os.ModePerm); err != nil {
		return fmt.Errorf("failed to write iop: %v", err)
	}

	return nil
}

func createCrossNetworkGateway(cluster kube.Cluster, cfg Config) error {
	scopes.Framework.Infof("Setting up cross-network-gateway in cluster: %s namespace: %s", cluster.Name(), cfg.SystemNamespace)
	_, err := cluster.ApplyContents(cfg.SystemNamespace, fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cross-network-gateway
  namespace: %s
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 443
      name: tls
      protocol: TLS
    tls:
      mode: AUTO_PASSTHROUGH
    hosts:
    - "*.local"
`, cfg.SystemNamespace))
	return err
}

func deployControlPlane(c *operatorComponent, cfg Config, cluster kube.Cluster, iopFile string) (err error) {
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(c.ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return err
	}
	defaultsIOPFile := cfg.IOPFile
	if !path.IsAbs(defaultsIOPFile) {
		defaultsIOPFile = filepath.Join(env.IstioSrc, defaultsIOPFile)
	}

	installSettings := []string{
		"-f", defaultsIOPFile,
		"-f", iopFile,
		"--set", "values.global.imagePullPolicy=" + s.PullPolicy,
		"--charts", filepath.Join(env.IstioSrc, "manifests"),
	}
	// Include all user-specified values.
	for k, v := range cfg.Values {
		installSettings = append(installSettings, "--set", fmt.Sprintf("values.%s=%s", k, v))
	}

	if c.environment.IsMulticluster() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+cluster.Name())

		if networkName := cluster.NetworkName(); networkName != "" {
			installSettings = append(installSettings, "--set", "values.global.meshID="+meshID,
				"--set", "values.global.network="+networkName)
		}

		if c.environment.IsControlPlaneCluster(cluster) {
			// Expose Istiod through ingress to allow remote clusters to connect
			installSettings = append(installSettings, "--set", "values.global.meshExpansion.enabled=true")
		} else {
			installSettings = append(installSettings, "--set", "profile=remote")
			controlPlaneCluster, err := c.environment.GetControlPlaneCluster(cluster)
			if err != nil {
				return fmt.Errorf("failed getting control plane cluster for cluster %d: %v", cluster.Index(), err)
			}
			var remoteIstiodAddress net.TCPAddr
			if err := retry.UntilSuccess(func() error {
				var err error
				remoteIstiodAddress, err = GetRemoteDiscoveryAddress(cfg.SystemNamespace, controlPlaneCluster.(kube.Cluster), false)
				return err
			}, retry.Timeout(1*time.Minute)); err != nil {
				return fmt.Errorf("failed getting the istiod address for cluster %d: %v", controlPlaneCluster.Index(), err)
			}
			installSettings = append(installSettings,
				"--set", "values.global.remotePilotAddress="+remoteIstiodAddress.IP.String(),
				// Use the local Istiod for CA
				"--set", "values.global.caAddress="+"istiod.istio-system.svc:15012")
		}
	}

	// Save the manifest generate output so we can later cleanup
	genCmd := []string{"manifest", "generate"}
	genCmd = append(genCmd, installSettings...)
	out, _, err := istioCtl.Invoke(genCmd)
	if err != nil {
		return err
	}
	c.saveInstallManifest(cluster.Name(), out)

	// Actually run the manifest apply command
	cmd := []string{
		"manifest", "apply",
		"--skip-confirmation",
	}
	cmd = append(cmd, installSettings...)
	scopes.Framework.Infof("Running istio control plane on cluster %s %v", cluster.Name(), cmd)
	if _, _, err := istioCtl.Invoke(cmd); err != nil {
		return fmt.Errorf("manifest apply failed: %v", err)
	}

	return nil
}

// meshNetworkSettings builds the values for meshNetworks with an endpoint in each network per-cluster.
// Assumes that the registry service is always istio-ingressgateway.
func meshNetworkSettings(cfg Config, environment *kube.Environment) *meshAPI.MeshNetworks {
	meshNetworks := meshAPI.MeshNetworks{Networks: make(map[string]*meshAPI.Network)}
	defaultGateways := []*meshAPI.Network_IstioNetworkGateway{{
		Gw: &meshAPI.Network_IstioNetworkGateway_RegistryServiceName{
			RegistryServiceName: "istio-ingressgateway." + cfg.IngressNamespace + ".svc.cluster.local",
		},
		Port: 443,
	}}

	for networkName, clusters := range environment.ClustersByNetwork() {
		network := &meshAPI.Network{
			Endpoints: make([]*meshAPI.Network_NetworkEndpoints, len(clusters)),
			Gateways:  defaultGateways,
		}
		for i, cluster := range clusters {
			network.Endpoints[i] = &meshAPI.Network_NetworkEndpoints{
				Ne: &meshAPI.Network_NetworkEndpoints_FromRegistry{
					FromRegistry: cluster.Name(),
				},
			}
		}
		meshNetworks.Networks[networkName] = network
	}

	return &meshNetworks
}

func waitForControlPlane(dumper resource.Dumper, cluster kube.Cluster, cfg Config) error {
	if !cfg.SkipWaitForValidationWebhook {
		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		if err := waitForValidationWebhook(cluster.Accessor, cfg); err != nil {
			dumper.Dump()
			return err
		}
	}
	return nil
}

func configureDirectAPIServerAccess(ctx resource.Context, env *kube.Environment, cfg Config) error {
	// Configure direct access for each control plane to each APIServer. This allows each control plane to
	// automatically discover endpoints in remote clusters.
	for _, cluster := range env.KubeClusters {
		// Create a secret.
		secret, err := createRemoteSecret(ctx, cluster)
		if err != nil {
			return fmt.Errorf("failed creating remote secret for cluster %s: %v", cluster.Name(), err)
		}

		// Copy this secret to all control plane clusters.
		for _, remote := range env.ControlPlaneClusters() {
			if cluster.Index() != remote.Index() {
				if _, err := remote.ApplyContents(cfg.SystemNamespace, secret); err != nil {
					return fmt.Errorf("failed applying remote secret to cluster %s: %v", remote.Name(), err)
				}
			}
		}
	}
	return nil
}

func createRemoteSecret(ctx resource.Context, cluster kube.Cluster) (string, error) {
	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return "", err
	}
	cmd := []string{
		"x", "create-remote-secret",
		"--name", cluster.Name(),
	}

	scopes.Framework.Infof("Creating remote secret for cluster cluster %d %v", cluster.Index(), cmd)
	out, _, err := istioCtl.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("create remote secret failed for cluster %d: %v", cluster.Index(), err)
	}
	return out, nil
}

func deployCACerts(workDir string, env *kube.Environment, cfg Config) error {
	certsDir := filepath.Join(workDir, "cacerts")
	if err := os.Mkdir(certsDir, 0700); err != nil {
		return err
	}

	root, err := ca.NewRoot(certsDir)
	if err != nil {
		return fmt.Errorf("failed creating the root CA: %v", err)
	}

	for _, cluster := range env.KubeClusters {
		// Create a subdir for the cluster certs.
		clusterDir := filepath.Join(certsDir, cluster.Name())
		if err := os.Mkdir(clusterDir, 0700); err != nil {
			return err
		}

		// Create the new extensions config for the CA
		caConfig, err := ca.NewIstioConfig(cfg.SystemNamespace)
		if err != nil {
			return err
		}

		// Create the certs for the cluster.
		clusterCA, err := ca.NewIntermediate(clusterDir, caConfig, root)
		if err != nil {
			return fmt.Errorf("failed creating intermediate CA for cluster %s: %v", cluster.Name(), err)
		}

		// Create the CA secret for this cluster. Istio will use these certs for its CA rather
		// than its autogenerated self-signed root.
		secret, err := clusterCA.NewIstioCASecret()
		if err != nil {
			return fmt.Errorf("failed creating intermediate CA secret for cluster %s: %v", cluster.Name(), err)
		}

		// Create the system namespace.
		if err := cluster.CreateNamespaceWithLabels(cfg.SystemNamespace, "", nil); err != nil {
			scopes.Framework.Infof("failed creating namespace %s on cluster %s. This can happen when deploying "+
				"multiple control planes. Error: %v", cfg.SystemNamespace, cluster.Name(), err)
		}

		// Create the secret for the cacerts.
		if _, err := cluster.CoreV1().Secrets(cfg.SystemNamespace).Create(context.TODO(), secret,
			kubeApiMeta.CreateOptions{}); err != nil {
			scopes.Framework.Infof("failed to create CA secrets on cluster %s. This can happen when deploying "+
				"multiple control planes. Error: %v", cluster.Name(), err)
		}
	}
	return nil
}

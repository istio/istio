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

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"
	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	meshAPI "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/istioctl/pkg/multicluster"
	pkgAPI "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/test/cert/ca"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// TODO: dynamically generate meshID to support multi-tenancy tests
const (
	meshID        = "testmesh0"
	istiodSvcName = "istiod"
)

var (
	// the retry options for waiting for an individual component to be ready
	componentDeployTimeout = retry.Timeout(30 * time.Second)
	componentDeployDelay   = retry.Delay(1 * time.Second)
)

type operatorComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment

	mu sync.Mutex
	// installManifest includes the yamls use to install Istio. These can be deleted on cleanup
	// The key is the cluster name
	installManifest map[string][]string
	ingress         map[resource.ClusterIndex]map[string]ingress.Instance
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

type istioctlConfigFiles struct {
	iopFile       string
	configIopFile string
	remoteIopFile string
}

func (i *operatorComponent) IngressFor(cluster resource.Cluster) ingress.Instance {
	return i.CustomIngressFor(cluster, defaultIngressServiceName, defaultIngressIstioLabel)
}

func (i *operatorComponent) CustomIngressFor(cluster resource.Cluster, serviceName, istioLabel string) ingress.Instance {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ingress[cluster.Index()] == nil {
		i.ingress[cluster.Index()] = map[string]ingress.Instance{}
	}
	if _, ok := i.ingress[cluster.Index()][istioLabel]; !ok {
		i.ingress[cluster.Index()][istioLabel] = newIngress(i.ctx, ingressConfig{
			Namespace:   i.settings.IngressNamespace,
			Cluster:     cluster,
			ServiceName: serviceName,
			IstioLabel:  istioLabel,
		})
	}
	return i.ingress[cluster.Index()][istioLabel]
}

func (i *operatorComponent) Close() (err error) {
	scopes.Framework.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	defer scopes.Framework.Infof("=== DONE: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)
	if i.settings.DeployIstio {
		for _, cluster := range i.environment.KubeClusters {
			for _, manifest := range i.installManifest[cluster.Name()] {
				if e := i.ctx.Config(cluster).DeleteYAML("", removeCRDs(manifest)); e != nil {
					err = multierror.Append(err, e)
				}
			}

			// Clean up dynamic leader election locks. This allows new test suites to become the leader without waiting 30s
			for _, cm := range leaderElectionConfigMaps {
				if e := cluster.CoreV1().ConfigMaps(i.settings.SystemNamespace).Delete(context.TODO(), cm,
					kubeApiMeta.DeleteOptions{}); e != nil {
					err = multierror.Append(err, e)
				}
			}
			if i.environment.IsMulticluster() {
				if e := cluster.CoreV1().Namespaces().Delete(context.TODO(), i.settings.SystemNamespace,
					kube2.DeleteOptionsForeground()); e != nil {
					err = multierror.Append(err, e)
				}
				if e := kube2.WaitForNamespaceDeletion(cluster, i.settings.SystemNamespace, retry.Timeout(time.Minute)); e != nil {
					err = multierror.Append(err, e)
				}
			}
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ingress != nil {
		for _, ings := range i.ingress {
			for _, ii := range ings {
				ii.CloseClients()
			}
		}
	}
	return
}

func (i *operatorComponent) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State...")

	for _, cluster := range i.environment.KubeClusters {
		d, err := ctx.CreateTmpDirectory(fmt.Sprintf("istio-state-%s", cluster.Name()))
		if err != nil {
			scopes.Framework.Errorf("Unable to create directory for dumping Istio contents: %v", err)
			return
		}
		kube2.DumpPods(cluster, d, i.settings.SystemNamespace)
	}
}

// saveManifestForCleanup will ensure we delete the given yaml from the given cluster during cleanup.
func (i *operatorComponent) saveManifestForCleanup(clusterName string, yaml string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.installManifest[clusterName] = append(i.installManifest[clusterName], yaml)
}

func deploy(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	scopes.Framework.Infof("=== Istio Component Config ===")
	scopes.Framework.Infof("\n%s", cfg.String())
	scopes.Framework.Infof("================================")

	i := &operatorComponent{
		environment:     env,
		settings:        cfg,
		ctx:             ctx,
		installManifest: map[string][]string{},
		ingress:         map[resource.ClusterIndex]map[string]ingress.Instance{},
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

	//generate istioctl config files for config, control plane(primary) and remote clusters
	istioctlConfigFiles, err := createIstioctlConfigFile(workDir, cfg, env)
	if err != nil {
		return nil, err
	}

	// For multicluster, create and push the CA certs to all clusters to establish a shared root of trust.
	if env.IsMulticluster() {
		if err := deployCACerts(workDir, env, cfg); err != nil {
			return nil, err
		}
	}

	// install config cluster
	for _, cluster := range env.KubeClusters {
		if env.IsConfigCluster(cluster) && !env.IsControlPlaneCluster(cluster) {
			if err = installRemoteConfigCluster(i, cfg, cluster, istioctlConfigFiles.configIopFile); err != nil {
				return i, err
			}
		}
	}

	// install control plane clusters (can be external or primary)
	errG := multierror.Group{}
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster) {
			cluster := cluster
			if err = installControlPlaneCluster(i, cfg, cluster, istioctlConfigFiles.iopFile); err != nil {
				return i, err
			}
		}
	}

	// Deploy Istio to remote clusters
	// Under external control plane mode, we only use config and external control plane(primary)clusters for now
	if !i.isExternalControlPlane() {
		// TODO allow doing this with external control planes (not implemented yet)

		// Remote secrets must be present for remote install to succeed
		if env.IsMulticluster() {
			// For multicluster, configure direct access so each control plane can get endpoints from all
			// API servers.
			if err := i.configureDirectAPIServerAccess(ctx, env, cfg); err != nil {
				return nil, err
			}
		}
		errG = multierror.Group{}
		for _, cluster := range env.KubeClusters {
			if !(env.IsControlPlaneCluster(cluster) || env.IsConfigCluster(cluster)) {
				cluster := cluster
				errG.Go(func() error {
					if err := installRemoteClusters(i, cfg, cluster, istioctlConfigFiles.remoteIopFile); err != nil {
						return fmt.Errorf("failed deploying control plane to remote cluster %s: %v", cluster.Name(), err)
					}
					return nil
				})
			}
		}
		if errs := errG.Wait(); errs != nil {
			return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
		}
	}

	if env.IsMultinetwork() {
		// enable cross network traffic
		for _, cluster := range env.KubeClusters {
			if err := i.applyCrossNetworkGateway(cluster); err != nil {
				return nil, err
			}
		}
	}

	return i, nil
}

func patchIstiodCustomHost(istiodAddress net.TCPAddr, cfg Config, cluster resource.Cluster) error {
	patchOptions := kubeApiMeta.PatchOptions{
		FieldManager: "istio-ci",
		TypeMeta: kubeApiMeta.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
	}
	contents := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: discovery
        env:
        - name: ISTIOD_CUSTOM_HOST
          value: %s
`, istiodAddress.IP.String())
	if _, err := cluster.AppsV1().Deployments(cfg.ConfigNamespace).Patch(context.TODO(), "istiod", types.ApplyPatchType,
		[]byte(contents), patchOptions); err != nil {
		return fmt.Errorf("failed to patch istiod with ISTIOD_CUSTOM_HOST: %v", err)
	}

	if err := retry.UntilSuccess(func() error {
		pods, err := cluster.CoreV1().Pods(cfg.SystemNamespace).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: "app=istiod"})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("no istiod pods")
		}
		for _, p := range pods.Items {
			for _, c := range p.Spec.Containers {
				if c.Name != "discovery" {
					continue
				}
				found := false
				for _, envVar := range c.Env {
					if envVar.Name == "ISTIOD_CUSTOM_HOST" {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("%v does not have ISTIOD_CUSTOM_HOST set", p.Name)
				}
			}
		}
		return nil
	}, componentDeployTimeout, componentDeployDelay); err != nil {
		return fmt.Errorf("failed waiting for patched istiod pod to come up in %s: %v", cluster.Name(), err)
	}

	return nil
}

func initIOPFile(cfg Config, env *kube.Environment, iopFile string, valuesYaml string) error {
	operatorYaml := cfg.IstioOperatorConfigYAML(valuesYaml)

	operatorCfg := &pkgAPI.IstioOperator{}
	if err := gogoprotomarshal.ApplyYAML(operatorYaml, operatorCfg); err != nil {
		return fmt.Errorf("failed to unmarshal base iop: %v, %v", err, operatorYaml)
	}
	var values = &pkgAPI.Values{}
	if operatorCfg.Spec.Values != nil {
		valuesYml, err := yaml.Marshal(operatorCfg.Spec.Values)
		if err != nil {
			return fmt.Errorf("failed to marshal base values: %v", err)
		}
		if err := gogoprotomarshal.ApplyYAML(string(valuesYml), values); err != nil {
			return fmt.Errorf("failed to unmarshal base values: %v", err)
		}
	}
	externalControlPlane := false
	for _, cluster := range env.KubeClusters {
		if env.IsControlPlaneCluster(cluster) && !env.IsConfigCluster(cluster) {
			externalControlPlane = true
		}
	}
	if env.IsMultinetwork() && !externalControlPlane {
		if values.Global == nil {
			values.Global = &pkgAPI.GlobalConfig{}
		}
		if values.Global.MeshNetworks == nil {
			meshNetworks, err := gogoprotomarshal.ToJSONMap(meshNetworkSettings(cfg, env))
			if err != nil {
				return fmt.Errorf("failed to convert meshNetworks: %v", err)
			}
			values.Global.MeshNetworks = meshNetworks["networks"].(map[string]interface{})
		}
	}

	valuesMap, err := gogoprotomarshal.ToJSONMap(values)
	if err != nil {
		return fmt.Errorf("failed to convert values to json map: %v", err)
	}
	operatorCfg.Spec.Values = valuesMap

	// marshaling entire operatorCfg causes panic because of *time.Time in ObjectMeta
	out, err := gogoprotomarshal.ToYAML(operatorCfg.Spec)
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

// installRemoteConfigCluster installs istio to a cluster that runs workloads and provides Istio configuration.
// The installed components include gateway deployments, CRDs, Roles, etc. but not istiod.
func installRemoteConfigCluster(i *operatorComponent, cfg Config, cluster resource.Cluster, configIopFile string) error {
	scopes.Framework.Infof("setting up %s as config cluster", cluster.Name())
	// TODO move values out of external istiod test main into the ConfigClusterValues defaults
	// TODO(cont) this method should just deploy the "base" resources needed to allow istio to read from k8s
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, configIopFile)
	if err != nil {
		return err
	}
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}

	err = install(i, installSettings, istioCtl, cluster.Name())
	if err != nil {
		return err
	}
	return nil
}

// installControlPlaneCluster installs the istiod control plane to the given cluster.
// The cluster is considered a "primary" cluster if it is also a "config cluster", in which case components
// like ingress will be installed.
func installControlPlaneCluster(i *operatorComponent, cfg Config, cluster resource.Cluster, iopFile string) error {
	scopes.Framework.Infof("setting up %s as control-plane cluster", cluster.Name())

	if !i.environment.IsConfigCluster(cluster) {
		if err := i.configureRemoteConfigForControlPlane(cluster); err != nil {
			return err
		}
	}
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, iopFile)
	if err != nil {
		return err
	}

	// TODO the external control plane clusterName should actually use its config cluster's name
	if i.environment.IsMulticluster() && !i.isExternalControlPlane() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+cluster.Name())
	}
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}

	err = install(i, installSettings, istioCtl, cluster.Name())
	if err != nil {
		return err
	}
	var istiodAddress net.TCPAddr
	if !i.environment.IsConfigCluster(cluster) {
		configCluster, err := i.environment.GetConfigCluster(cluster)
		if err != nil {
			return err
		}
		istiodAddress, err = i.RemoteDiscoveryAddressFor(configCluster)
		if err != nil {
			return err
		}
		if err := patchIstiodCustomHost(istiodAddress, cfg, cluster); err != nil {
			return err
		}
		if err := configureDiscoveryForConfigCluster(istiodAddress.IP.String(), cfg, configCluster); err != nil {
			return err
		}
	}

	if i.environment.IsConfigCluster(cluster) {
		if err := i.deployEastWestGateway(cluster); err != nil {
			return err
		}
		// Other clusters should only use this for discovery if its a config cluster.
		if err := i.applyIstiodGateway(cluster); err != nil {
			return fmt.Errorf("failed applying istiod gateway for cluster %s: %v", cluster.Name(), err)
		}
		if err := waitForIstioReady(i.ctx, cluster, cfg); err != nil {
			return err
		}
	}

	return nil
}

// Deploy Istio to remote clusters
func installRemoteClusters(i *operatorComponent, cfg Config, cluster resource.Cluster, remoteIopFile string) error {
	// TODO this method should handle setting up discovery from remote config clusters to their control-plane
	// TODO(cont) and eventually we should always use istiod-less remotes
	scopes.Framework.Infof("setting up %s as remote cluster", cluster.Name())
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, remoteIopFile)
	if err != nil {
		return err
	}
	if i.environment.IsMulticluster() && !i.isExternalControlPlane() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+cluster.Name())
	}
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}
	if !i.isExternalControlPlane() {
		remoteIstiodAddress, err := i.RemoteDiscoveryAddressFor(cluster)
		if err != nil {
			return err
		}
		installSettings = append(installSettings,
			"--set", "values.global.remotePilotAddress="+remoteIstiodAddress.IP.String())

		if isCentralIstio(i.environment, cfg) {
			// TODO allow all remotes to use custom injection URLs
			installSettings = append(installSettings,
				"--set", fmt.Sprintf("values.istiodRemote.injectionURL=https://%s:%d/inject", remoteIstiodAddress.IP.String(), 15017),
				"--set", fmt.Sprintf("values.base.validationURL=https://%s:%d/validate", remoteIstiodAddress.IP.String(), 15017))
		}
	}

	if err := install(i, installSettings, istioCtl, cluster.Name()); err != nil {
		return err
	}

	// remote clusters only need this gateway for multi-network purposes
	if i.ctx.Environment().IsMultinetwork() {
		if err := i.deployEastWestGateway(cluster); err != nil {
			return err
		}
	}

	return nil
}

func (i *operatorComponent) generateCommonInstallSettings(cfg Config, cluster resource.Cluster, iopFile string) ([]string, error) {

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return nil, err
	}
	defaultsIOPFile := cfg.IOPFile
	if !path.IsAbs(defaultsIOPFile) {
		defaultsIOPFile = filepath.Join(testenv.IstioSrc, defaultsIOPFile)
	}

	installSettings := []string{
		"-f", defaultsIOPFile,
		"-f", iopFile,
		"--set", "values.global.imagePullPolicy=" + s.PullPolicy,
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
	}

	if i.environment.IsMultinetwork() && cluster.NetworkName() != "" {
		installSettings = append(installSettings,
			"--set", "values.global.meshID="+meshID,
			"--set", "values.global.network="+cluster.NetworkName())
	}

	// Include all user-specified values.
	for k, v := range cfg.Values {
		installSettings = append(installSettings, "--set", fmt.Sprintf("values.%s=%s", k, v))
	}
	return installSettings, nil
}

func isCentralIstio(env *kube.Environment, cfg Config) bool {
	if env.IsMulticluster() && cfg.Values["global.centralIstiod"] == "true" {
		return true
	}
	return false
}

// install will replace and reconcile the installation based on the given install settings
func install(c *operatorComponent, installSettings []string, istioCtl istioctl.Instance, clusterName string) error {
	// Save the manifest generate output so we can later cleanup
	genCmd := []string{"manifest", "generate"}
	genCmd = append(genCmd, installSettings...)
	out, _, err := istioCtl.Invoke(genCmd)
	if err != nil {
		return err
	}
	c.saveManifestForCleanup(clusterName, out)

	// Actually run the install command
	cmd := []string{
		"install",
		"--skip-confirmation",
	}
	cmd = append(cmd, installSettings...)
	scopes.Framework.Infof("Running istio control plane on cluster %s %v", clusterName, cmd)
	if _, _, err := istioCtl.Invoke(cmd); err != nil {
		return fmt.Errorf("install failed: %v", err)
	}
	return nil
}

// meshNetworkSettings builds the values for meshNetworks with an endpoint in each network per-cluster.
// Assumes that the registry service is always istio-ingressgateway.
func meshNetworkSettings(cfg Config, environment *kube.Environment) *meshAPI.MeshNetworks {
	meshNetworks := meshAPI.MeshNetworks{Networks: make(map[string]*meshAPI.Network)}
	defaultGateways := []*meshAPI.Network_IstioNetworkGateway{{
		Gw: &meshAPI.Network_IstioNetworkGateway_RegistryServiceName{
			RegistryServiceName: eastWestIngressServiceName + "." + cfg.IngressNamespace + ".svc.cluster.local",
		},
		Port: 15443, // should be the mTLS port on east-west gateway (see samples/multicluster/eastwest-gateway.yaml)
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

func waitForIstioReady(ctx resource.Context, cluster resource.Cluster, cfg Config) error {
	if !cfg.SkipWaitForValidationWebhook {
		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		if err := waitForValidationWebhook(ctx, cluster, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServerAccess(ctx resource.Context, env *kube.Environment, cfg Config) error {
	// Configure direct access for each control plane to each APIServer. This allows each control plane to
	// automatically discover endpoints in remote clusters.
	for _, cluster := range env.KubeClusters {
		if err := i.configureDirectAPIServiceAccessForCluster(ctx, env, cfg, cluster); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServiceAccessForCluster(ctx resource.Context, env *kube.Environment, cfg Config,
	cluster resource.Cluster) error {
	// Ensure the istio-reader-service-account exists
	if err := i.createServiceAccount(ctx, cluster); err != nil {
		return err
	}
	// Create a secret.
	secret, err := createRemoteSecret(ctx, cluster, cfg)
	if err != nil {
		return fmt.Errorf("failed creating remote secret for cluster %s: %v", cluster.Name(), err)
	}
	if err := ctx.Config(env.ControlPlaneClusters(cluster)...).ApplyYAML(cfg.SystemNamespace, secret); err != nil {
		return fmt.Errorf("failed applying remote secret to clusters: %v", err)
	}
	return nil
}

func (i *operatorComponent) createServiceAccount(ctx resource.Context, cluster resource.Cluster) error {
	// TODO(landow) we should not have users do this. instead this could be a part of create-remote-secret
	sa, err := cluster.CoreV1().ServiceAccounts("istio-system").
		Get(context.TODO(), multicluster.DefaultServiceAccountName, kubeApiMeta.GetOptions{})
	if err == nil && sa != nil {
		scopes.Framework.Infof("service account exists in %s", cluster.Name())
		return nil
	}
	scopes.Framework.Infof("creating service account in %s", cluster.Name())

	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return err
	}
	return install(i, []string{
		"--set", "profile=empty",
		"--set", "components.base.enabled=true",
		"--set", "values.global.configValidation=false",
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
	}, istioCtl, cluster.Name())
}

func createRemoteSecret(ctx resource.Context, cluster resource.Cluster, cfg Config) (string, error) {
	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: cluster,
	})
	if err != nil {
		return "", err
	}
	cmd := []string{
		"x", "create-remote-secret",
		"--name", cluster.Name(),
		"--namespace", cfg.SystemNamespace,
	}

	scopes.Framework.Infof("Creating remote secret for cluster cluster %s %v", cluster.Name(), cmd)
	out, _, err := istioCtl.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("create remote secret failed for cluster %s: %v", cluster.Name(), err)
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
		if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name: cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
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

func configureDiscoveryForConfigCluster(discoveryAddress string, cfg Config, cluster resource.Cluster) error {
	scopes.Framework.Infof("creating endpoints and service in %s to get discovery from %s", cluster.Name(), discoveryPort)
	svc := &kubeApiCore.Service{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:      istiodSvcName,
			Namespace: cfg.SystemNamespace,
		},
		Spec: kubeApiCore.ServiceSpec{
			Ports: []kubeApiCore.ServicePort{
				{
					Port:     15012,
					Name:     "tcp-istiod",
					Protocol: kubeApiCore.ProtocolTCP,
				},
				{
					Name:     "tcp-webhook",
					Protocol: kubeApiCore.ProtocolTCP,
					Port:     15017,
				},
			},
			ClusterIP: "None",
		},
	}
	_, err := cluster.CoreV1().Services(cfg.SystemNamespace).Create(context.TODO(), svc, kubeApiMeta.CreateOptions{})
	if err != nil {
		return err
	}
	eps := &kubeApiCore.Endpoints{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:      istiodSvcName,
			Namespace: cfg.SystemNamespace,
		},
		Subsets: []kubeApiCore.EndpointSubset{
			{
				Addresses: []kubeApiCore.EndpointAddress{
					{
						IP: discoveryAddress,
					},
				},
				Ports: []kubeApiCore.EndpointPort{
					{
						Name:     "tcp-istiod",
						Protocol: kubeApiCore.ProtocolTCP,
						Port:     15012,
					},
					{
						Name:     "tcp-webhook",
						Protocol: kubeApiCore.ProtocolTCP,
						Port:     15017,
					},
				},
			},
		},
	}

	_, err = cluster.CoreV1().Endpoints(cfg.SystemNamespace).Create(context.TODO(), eps, kubeApiMeta.CreateOptions{})
	if err != nil {
		return err
	}
	err = retry.UntilSuccess(func() error {
		_, err := cluster.CoreV1().Services(cfg.SystemNamespace).Get(context.TODO(), istiodSvcName, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		_, err = cluster.CoreV1().Endpoints(cfg.SystemNamespace).Get(context.TODO(), istiodSvcName, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		return nil
	}, componentDeployTimeout, componentDeployDelay)
	return err
}

// configureRemoteConfigForControlPlane allows istiod in the given external control plane to read resources
// in its remote config cluster by creating the kubeconfig secret pointing to the remote kubeconfig, and the
// service account required to read the secret.
func (i *operatorComponent) configureRemoteConfigForControlPlane(cluster resource.Cluster) error {
	env, cfg := i.environment, i.settings
	configCluster, err := env.GetConfigCluster(cluster)
	if err != nil {
		return err
	}
	istioKubeConfig, err := file.AsString(env.Settings().KubeConfig[configCluster.Index()])
	if err != nil {
		scopes.Framework.Infof("error in parsing kubeconfig for %s", configCluster.Name())
		return err
	}

	scopes.Framework.Infof("configuring config cluster %s to use external control plane in %s", cluster.Name())
	// ensure system namespace exists
	_, err = cluster.CoreV1().Namespaces().
		Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name: cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{})
	if !errors.IsAlreadyExists(err) {
		return err
	}
	// create kubeconfig secret
	_, err = cluster.CoreV1().Secrets(cfg.SystemNamespace).
		Create(context.TODO(), &kubeApiCore.Secret{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:      "istio-kubeconfig",
				Namespace: cfg.SystemNamespace,
			},
			Data: map[string][]byte{
				"config": []byte(istioKubeConfig),
			},
		}, kubeApiMeta.CreateOptions{})
	if err != nil {
		scopes.Framework.Infof("has error in creating istio-kubeconfig secrets %v", err)
		return err
	}
	// create service account for reading the secrets
	_, err = cluster.CoreV1().ServiceAccounts(cfg.SystemNamespace).
		Create(context.TODO(), &kubeApiCore.ServiceAccount{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Namespace: cfg.SystemNamespace,
				Name:      "istiod-service-account",
			},
		}, kubeApiMeta.CreateOptions{})
	if err != nil {
		scopes.Framework.Infof("has error in creating istiod service account %v", err)
		return err
	}

	return nil
}

func createIstioctlConfigFile(workDir string, cfg Config, env *kube.Environment) (istioctlConfigFiles, error) {
	configFiles := istioctlConfigFiles{
		iopFile:       "",
		configIopFile: "",
		remoteIopFile: "",
	}
	// Generate the istioctl config file for control plane(primary) cluster
	configFiles.iopFile = filepath.Join(workDir, "iop.yaml")
	if err := initIOPFile(cfg, env, configFiles.iopFile, cfg.ControlPlaneValues); err != nil {
		return configFiles, err
	}

	// Generate the istioctl config file for remote cluster
	configFiles.remoteIopFile = configFiles.iopFile
	if cfg.RemoteClusterValues != "" {
		configFiles.remoteIopFile = filepath.Join(workDir, "remote.yaml")
		if err := initIOPFile(cfg, env, configFiles.remoteIopFile, cfg.RemoteClusterValues); err != nil {
			return configFiles, err
		}
	}

	// Generate the istioctl config file for config cluster
	configFiles.configIopFile = configFiles.iopFile
	if cfg.ConfigClusterValues != "" {
		configFiles.configIopFile = filepath.Join(workDir, "config.yaml")
		if err := initIOPFile(cfg, env, configFiles.configIopFile, cfg.ConfigClusterValues); err != nil {
			return configFiles, err
		}
	}
	return configFiles, nil
}

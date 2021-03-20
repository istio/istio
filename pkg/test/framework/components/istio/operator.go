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
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"
	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	opAPI "istio.io/api/operator/v1alpha1"
	pkgAPI "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/test/cert/ca"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
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
	componentDeployTimeout = retry.Timeout(1 * time.Minute)
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
	// ingress components, indexed first by cluster name and then by gateway name.
	ingress    map[string]map[string]ingress.Instance
	workDir    string
	deployTime time.Duration
}

var (
	_ io.Closer       = &operatorComponent{}
	_ Instance        = &operatorComponent{}
	_ resource.Dumper = &operatorComponent{}
)

// ID implements resource.Instance
func (i *operatorComponent) ID() resource.ID {
	return i.id
}

func (i *operatorComponent) Settings() Config {
	return i.settings
}

func removeCRDsSlice(raw []string) string {
	res := []string{}
	for _, r := range raw {
		res = append(res, removeCRDs(r))
	}
	return yml.JoinString(res...)
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

type istioctlConfigFiles struct {
	iopFile            string
	operatorSpec       *opAPI.IstioOperatorSpec
	configIopFile      string
	configOperatorSpec *opAPI.IstioOperatorSpec
	remoteIopFile      string
	remoteOperatorSpec *opAPI.IstioOperatorSpec
}

func (i *operatorComponent) IngressFor(cluster cluster.Cluster) ingress.Instance {
	return i.CustomIngressFor(cluster, defaultIngressServiceName, defaultIngressIstioLabel)
}

func (i *operatorComponent) CustomIngressFor(c cluster.Cluster, serviceName, istioLabel string) ingress.Instance {
	i.mu.Lock()
	defer i.mu.Unlock()
	if c.Kind() != cluster.Kubernetes {
		c = c.Primary()
	}

	if i.ingress[c.Name()] == nil {
		i.ingress[c.Name()] = map[string]ingress.Instance{}
	}
	if _, ok := i.ingress[c.Name()][istioLabel]; !ok {
		i.ingress[c.Name()][istioLabel] = newIngress(i.ctx, ingressConfig{
			Namespace:   i.settings.IngressNamespace,
			Cluster:     c,
			ServiceName: serviceName,
			IstioLabel:  istioLabel,
		})
	}
	return i.ingress[c.Name()][istioLabel]
}

func appendToFile(contents string, file string) error {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}

	defer f.Close()

	if _, err = f.WriteString(contents); err != nil {
		return err
	}
	return nil
}

func (i *operatorComponent) Close() error {
	t0 := time.Now()
	scopes.Framework.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)

	// Write time spent for cleanup and deploy to ARTIFACTS/trace.yaml and logs to allow analyzing test times
	defer func() {
		delta := time.Since(t0)
		y := fmt.Sprintf(`'suite/%s':
  istio-deploy: %f
  istio-cleanup: %f
`, i.ctx.Settings().TestID, i.deployTime.Seconds(), delta.Seconds())
		_ = appendToFile(y, filepath.Join(i.ctx.Settings().BaseDir, "trace.yaml"))
		scopes.Framework.Infof("=== SUCCEEDED: Cleanup Istio in %v [Suite=%s] ===", delta, i.ctx.Settings().TestID)
	}()

	if i.settings.DumpKubernetesManifests {
		i.dumpGeneratedManifests()
	}

	if i.settings.DeployIstio {
		errG := multierror.Group{}
		for _, cluster := range i.ctx.Clusters().Kube() {
			cluster := cluster
			errG.Go(func() (err error) {
				if e := i.ctx.Config(cluster).DeleteYAML("", removeCRDsSlice(i.installManifest[cluster.Name()])); e != nil {
					err = multierror.Append(err, e)
				}
				// Cleanup all secrets and configmaps - these are dynamically created by tests and/or istiod so they are not captured above
				// This includes things like leader election locks (allowing next test to start without 30s delay),
				// custom cacerts, custom kubeconfigs, etc.
				// We avoid deleting the whole namespace since its extremely slow in Kubernetes (30-60s+)
				if e := cluster.CoreV1().Secrets(i.settings.SystemNamespace).DeleteCollection(
					context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
					err = multierror.Append(err, e)
				}
				if e := cluster.CoreV1().ConfigMaps(i.settings.SystemNamespace).DeleteCollection(
					context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
					err = multierror.Append(err, e)
				}
				return
			})
		}
		return errG.Wait().ErrorOrNil()
	}
	return nil
}

func (i *operatorComponent) dumpGeneratedManifests() {
	manifestsDir := path.Join(i.workDir, "manifests")
	if err := os.Mkdir(manifestsDir, 0o700); err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping install manifests: %v", err)
		return
	}
	for clusterName, manifests := range i.installManifest {
		clusterDir := path.Join(manifestsDir, clusterName)
		if err := os.Mkdir(manifestsDir, 0o700); err != nil {
			scopes.Framework.Errorf("Unable to create directory for dumping %s install manifests: %v", clusterName, err)
			return
		}
		for i, manifest := range manifests {
			err := ioutil.WriteFile(path.Join(clusterDir, "manifest-"+strconv.Itoa(i)+".yaml"), []byte(manifest), 0o644)
			if err != nil {
				scopes.Framework.Errorf("Failed writing manifest %d/%d in %s: %v", i, len(manifests)-1, clusterName, err)
			}
		}
	}
}

func (i *operatorComponent) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State...")
	ns := i.settings.SystemNamespace
	d, err := ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}
	kube2.DumpPods(ctx, d, ns)
	for _, cluster := range ctx.Clusters().Kube() {
		kube2.DumpDebug(cluster, d, "configz")
	}
}

// saveManifestForCleanup will ensure we delete the given yaml from the given cluster during cleanup.
func (i *operatorComponent) saveManifestForCleanup(clusterName string, yaml string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.installManifest[clusterName] = append(i.installManifest[clusterName], yaml)
}

func deploy(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	i := &operatorComponent{
		environment:     env,
		settings:        cfg,
		ctx:             ctx,
		installManifest: map[string][]string{},
		ingress:         map[string]map[string]ingress.Instance{},
	}
	if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
		cfg.RemoteClusterIOPFile = IntegrationTestIstiodlessRemoteDefaultsIOP
		i.settings = cfg
	}

	scopes.Framework.Infof("=== Istio Component Config ===")
	scopes.Framework.Infof("\n%s", cfg.String())
	scopes.Framework.Infof("================================")

	t0 := time.Now()
	defer func() {
		i.deployTime = time.Since(t0)
	}()
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
	i.workDir = workDir

	// generate istioctl config files for config, control plane(primary) and remote clusters
	istioctlConfigFiles, err := createIstioctlConfigFile(workDir, cfg)
	if err != nil {
		return nil, err
	}

	// For multicluster, create and push the CA certs to all clusters to establish a shared root of trust.
	if env.IsMulticluster() {
		if err := deployCACerts(workDir, env, cfg); err != nil {
			return nil, err
		}
	}

	// install remote config clusters, we do this first so that the external istiod has a place to read config from
	for _, cluster := range ctx.Clusters().Kube().Configs().Remotes() {
		if err = installRemoteConfigCluster(i, cfg, cluster, istioctlConfigFiles.configIopFile); err != nil {
			return i, err
		}
	}

	// install control plane clusters (can be external or primary)
	errG := multierror.Group{}
	for _, cluster := range ctx.Clusters().Kube().Primaries() {
		cluster := cluster
		errG.Go(func() error {
			return installControlPlaneCluster(i, cfg, cluster, istioctlConfigFiles.iopFile, istioctlConfigFiles.operatorSpec)
		})
	}
	if err := errG.Wait(); err != nil {
		scopes.Framework.Errorf("one or more errors occurred installing control-plane clusters: %v", err)
		return i, err
	}

	if ctx.Clusters().IsMulticluster() {
		// For multicluster, configure direct access so each control plane can get endpoints from all
		// API servers.
		if err := i.configureDirectAPIServerAccess(ctx, cfg); err != nil {
			return nil, err
		}
	}

	// Deploy Istio to remote clusters
	// Under external control plane mode, we only use config and external control plane(primary)clusters for now
	if !i.isExternalControlPlane() {
		// TODO allow remotes with an external control planes (not implemented yet)
		errG = multierror.Group{}
		for _, cluster := range ctx.Clusters().Kube().Remotes(ctx.Clusters().Configs()...) {
			cluster := cluster
			errG.Go(func() error {
				if err := installRemoteClusters(i, cfg, cluster, istioctlConfigFiles.remoteIopFile, istioctlConfigFiles.remoteOperatorSpec); err != nil {
					return fmt.Errorf("failed installing remote cluster %s: %v", cluster.Name(), err)
				}
				return nil
			})
		}
		if errs := errG.Wait(); errs != nil {
			return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
		}

		// TODO allow multi-network with an external control planes
		if env.IsMultinetwork() {
			// enable cross network traffic
			for _, cluster := range ctx.Clusters().Kube() {
				if err := i.exposeUserServices(cluster); err != nil {
					return nil, err
				}
			}
		}
	}

	return i, nil
}

// patchIstiodCustomHost sets the ISTIOD_CUSTOM_HOST to the given address,
// to allow webhook connections to succeed when reaching webhook by IP.
func patchIstiodCustomHost(istiodAddress net.TCPAddr, cfg Config, cluster cluster.Cluster) error {
	scopes.Framework.Infof("patching custom host for cluster %s as %s", cluster.Name(), istiodAddress.IP.String())
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

func initIOPFile(cfg Config, iopFile string, valuesYaml string) (*opAPI.IstioOperatorSpec, error) {
	operatorYaml := cfg.IstioOperatorConfigYAML(valuesYaml)

	operatorCfg := &pkgAPI.IstioOperator{}
	if err := gogoprotomarshal.ApplyYAML(operatorYaml, operatorCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base iop: %v, %v", err, operatorYaml)
	}
	values := &pkgAPI.Values{}
	if operatorCfg.Spec.Values != nil {
		valuesYml, err := yaml.Marshal(operatorCfg.Spec.Values)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal base values: %v", err)
		}
		if err := gogoprotomarshal.ApplyYAML(string(valuesYml), values); err != nil {
			return nil, fmt.Errorf("failed to unmarshal base values: %v", err)
		}
	}

	valuesMap, err := gogoprotomarshal.ToJSONMap(values)
	if err != nil {
		return nil, fmt.Errorf("failed to convert values to json map: %v", err)
	}
	operatorCfg.Spec.Values = valuesMap

	// marshaling entire operatorCfg causes panic because of *time.Time in ObjectMeta
	out, err := gogoprotomarshal.ToYAML(operatorCfg.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling iop spec: %v", err)
	}

	out = fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
%s`, Indent(out, "  "))

	if err := ioutil.WriteFile(iopFile, []byte(out), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write iop: %v", err)
	}

	return operatorCfg.Spec, nil
}

// installRemoteConfigCluster installs istio to a cluster that runs workloads and provides Istio configuration.
// The installed components include gateway deployments, CRDs, Roles, etc. but not istiod.
func installRemoteConfigCluster(i *operatorComponent, cfg Config, cluster cluster.Cluster, configIopFile string) error {
	scopes.Framework.Infof("setting up %s as config cluster", cluster.Name())
	// TODO move --set values out of external istiod test main into the ConfigClusterValues defaults
	// TODO(cont) this method should just deploy the "base" resources needed to allow istio to read from k8s
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, cfg.ConfigClusterIOPFile, configIopFile)
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
func installControlPlaneCluster(i *operatorComponent, cfg Config, cluster cluster.Cluster, iopFile string,
	spec *opAPI.IstioOperatorSpec) error {
	scopes.Framework.Infof("setting up %s as control-plane cluster", cluster.Name())

	if !cluster.IsConfig() {
		if err := i.configureRemoteConfigForControlPlane(cluster); err != nil {
			return err
		}
	}
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, cfg.PrimaryClusterIOPFile, iopFile)
	if err != nil {
		return err
	}

	// Set the clusterName for the local cluster.
	// This MUST match the clusterName in the remote secret for this cluster.
	if i.environment.IsMulticluster() {
		clusterName := cluster.Name()
		if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
			installSettings = append(installSettings, "--set", "values.global.externalIstiod=true")
		}
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+clusterName)
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
	if !cluster.IsConfig() {
		// this is an external control plane, patch the ISTIOD_CUSTOM_HOST to allow using the webhook by IP
		istiodAddress, err := i.RemoteDiscoveryAddressFor(cluster)
		if err != nil {
			return err
		}
		if err := patchIstiodCustomHost(istiodAddress, cfg, cluster); err != nil {
			return err
		}
		// create Serivice/Endpoints to allow accessing this istiod for discovery and webhook from the remote config cluster
		if err := configureRemoteConfigClusterDiscovery(istiodAddress.IP.String(), cfg, cluster.Config()); err != nil {
			return err
		}
	}

	if cluster.IsConfig() {
		// this is a traditional primary cluster, install the eastwest gateway

		// there are a few tests that require special gateway setup which will cause eastwest gateway fail to start
		// exclude these tests from installing eastwest gw for now
		if !cfg.DeployEastWestGW {
			return nil
		}

		if err := i.deployEastWestGateway(cluster, spec.Revision); err != nil {
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
func installRemoteClusters(i *operatorComponent, cfg Config, cluster cluster.Cluster, remoteIopFile string, spec *opAPI.IstioOperatorSpec) error {
	// TODO this method should handle setting up discovery from remote config clusters to their control-plane
	// TODO(cont) and eventually we should always use istiod-less remotes
	scopes.Framework.Infof("setting up %s as remote cluster", cluster.Name())
	installSettings, err := i.generateCommonInstallSettings(cfg, cluster, cfg.RemoteClusterIOPFile, remoteIopFile)
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

	// in external control plane, we've already created the Service/Endpoint, no need to set this up.
	if !i.isExternalControlPlane() {
		remoteIstiodAddress, err := i.RemoteDiscoveryAddressFor(cluster)
		if err != nil {
			return err
		}
		installSettings = append(installSettings, "--set", "values.global.remotePilotAddress="+remoteIstiodAddress.IP.String())
		if cfg.IstiodlessRemotes {
			installSettings = append(installSettings,
				"--set", fmt.Sprintf("values.istiodRemote.injectionURL=https://%s:%d/inject/net/%s/cluster/%s",
					remoteIstiodAddress.IP.String(), 15017, cluster.NetworkName(), cluster.Name()),
				"--set", fmt.Sprintf("values.base.validationURL=https://%s:%d/validate", remoteIstiodAddress.IP.String(), 15017))
		}
	}

	if err := install(i, installSettings, istioCtl, cluster.Name()); err != nil {
		return err
	}

	// remote clusters only need this gateway for multi-network purposes
	if i.ctx.Environment().IsMultinetwork() {
		if err := i.deployEastWestGateway(cluster, spec.Revision); err != nil {
			return err
		}
	}

	return nil
}

func (i *operatorComponent) generateCommonInstallSettings(cfg Config, cluster cluster.Cluster, defaultsIOPFile, iopFile string) ([]string, error) {
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return nil, err
	}
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
	scopes.Framework.Infof("Installing Istio components on cluster %s %v", clusterName, cmd)
	if _, _, err := istioCtl.Invoke(cmd); err != nil {
		return fmt.Errorf("install failed: %v", err)
	}
	return nil
}

func waitForIstioReady(ctx resource.Context, cluster cluster.Cluster, cfg Config) error {
	if !cfg.SkipWaitForValidationWebhook {
		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		if err := waitForValidationWebhook(ctx, cluster, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServerAccess(ctx resource.Context, cfg Config) error {
	// Configure direct access for each control plane to each APIServer. This allows each control plane to
	// automatically discover endpoints in remote clusters.
	for _, cluster := range ctx.Clusters().Kube() {
		if err := i.configureDirectAPIServiceAccessForCluster(ctx, cfg, cluster); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServiceAccessForCluster(ctx resource.Context, cfg Config,
	cluster cluster.Cluster) error {
	// Create a secret.
	secret, err := createRemoteSecret(ctx, cluster, cfg)
	if err != nil {
		return fmt.Errorf("failed creating remote secret for cluster %s: %v", cluster.Name(), err)
	}
	clusters := ctx.Clusters().Primaries(cluster)
	if len(clusters) == 0 {
		// giving 0 clusters to ctx.Config() means using all clusters
		return nil
	}
	if err := ctx.Config(clusters...).ApplyYAML(cfg.SystemNamespace, secret); err != nil {
		return fmt.Errorf("failed applying remote secret to clusters: %v", err)
	}
	return nil
}

func createRemoteSecret(ctx resource.Context, cluster cluster.Cluster, cfg Config) (string, error) {
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
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
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
	if err := os.Mkdir(certsDir, 0o700); err != nil {
		return err
	}

	root, err := ca.NewRoot(certsDir)
	if err != nil {
		return fmt.Errorf("failed creating the root CA: %v", err)
	}

	for _, cluster := range env.Clusters() {
		// Create a subdir for the cluster certs.
		clusterDir := filepath.Join(certsDir, cluster.Name())
		if err := os.Mkdir(clusterDir, 0o700); err != nil {
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
		var nsLabels map[string]string
		if env.IsMultinetwork() {
			nsLabels = map[string]string{label.TopologyNetwork.Name: cluster.NetworkName()}
		}
		if _, err := cluster.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Labels: nsLabels,
				Name:   cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := cluster.CoreV1().Namespaces().Update(context.TODO(), &kubeApiCore.Namespace{
					ObjectMeta: kubeApiMeta.ObjectMeta{
						Labels: nsLabels,
						Name:   cfg.SystemNamespace,
					},
				}, kubeApiMeta.UpdateOptions{}); err != nil {
					scopes.Framework.Errorf("failed updating namespace %s on cluster %s. This can happen when deploying "+
						"multiple control planes. Error: %v", cfg.SystemNamespace, cluster.Name(), err)
				}
			} else {
				scopes.Framework.Errorf("failed creating namespace %s on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", cfg.SystemNamespace, cluster.Name(), err)
			}
		}

		// Create the secret for the cacerts.
		if _, err := cluster.CoreV1().Secrets(cfg.SystemNamespace).Create(context.TODO(), secret,
			kubeApiMeta.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := cluster.CoreV1().Secrets(cfg.SystemNamespace).Update(context.TODO(), secret,
					kubeApiMeta.UpdateOptions{}); err != nil {
					scopes.Framework.Errorf("failed to create CA secrets on cluster %s. This can happen when deploying "+
						"multiple control planes. Error: %v", cluster.Name(), err)
				}
			} else {
				scopes.Framework.Errorf("failed to create CA secrets on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", cluster.Name(), err)
			}
		}
	}
	return nil
}

func configureRemoteConfigClusterDiscovery(discoveryAddress string, cfg Config, cluster cluster.Cluster) error {
	scopes.Framework.Infof("creating endpoints and service in %s to get discovery from %s", cluster.Name(), discoveryAddress)
	svc := &kubeApiCore.Service{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:      istiodSvcName,
			Namespace: cfg.SystemNamespace,
		},
		Spec: kubeApiCore.ServiceSpec{
			Ports: []kubeApiCore.ServicePort{
				{
					Port:     15012,
					Name:     "tls-istiod",
					Protocol: kubeApiCore.ProtocolTCP,
				},
				{
					Name:     "tls-webhook",
					Protocol: kubeApiCore.ProtocolTCP,
					Port:     15017,
				},
				{
					Name:     "tls",
					Protocol: kubeApiCore.ProtocolTCP,
					Port:     15443,
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
						Name:     "tls-istiod",
						Protocol: kubeApiCore.ProtocolTCP,
						Port:     15012,
					},
					{
						Name:     "tls-webhook",
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
func (i *operatorComponent) configureRemoteConfigForControlPlane(cluster cluster.Cluster) error {
	cfg := i.settings
	configCluster := cluster.Config()
	istioKubeConfig, err := file.AsString(configCluster.(*kubecluster.Cluster).Filename())
	if err != nil {
		scopes.Framework.Infof("error in parsing kubeconfig for %s", configCluster.Name())
		return err
	}

	scopes.Framework.Infof("configuring external control plane to use config cluster in %s", cluster.Name())
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

func createIstioctlConfigFile(workDir string, cfg Config) (istioctlConfigFiles, error) {
	var err error
	configFiles := istioctlConfigFiles{
		iopFile:       "",
		configIopFile: "",
		remoteIopFile: "",
	}
	// Generate the istioctl config file for control plane(primary) cluster
	configFiles.iopFile = filepath.Join(workDir, "iop.yaml")
	if configFiles.operatorSpec, err = initIOPFile(cfg, configFiles.iopFile, cfg.ControlPlaneValues); err != nil {
		return configFiles, err
	}

	// Generate the istioctl config file for remote cluster
	if cfg.RemoteClusterValues == "" {
		cfg.RemoteClusterValues = cfg.ControlPlaneValues
	}

	configFiles.remoteIopFile = filepath.Join(workDir, "remote.yaml")
	if configFiles.remoteOperatorSpec, err = initIOPFile(cfg, configFiles.remoteIopFile, cfg.RemoteClusterValues); err != nil {
		return configFiles, err
	}

	// Generate the istioctl config file for config cluster
	configFiles.configIopFile = configFiles.iopFile
	configFiles.configOperatorSpec = configFiles.operatorSpec
	if cfg.ConfigClusterValues != "" {
		configFiles.configIopFile = filepath.Join(workDir, "config.yaml")
		if configFiles.configOperatorSpec, err = initIOPFile(cfg, configFiles.configIopFile, cfg.ConfigClusterValues); err != nil {
			return configFiles, err
		}
	}
	return configFiles, nil
}

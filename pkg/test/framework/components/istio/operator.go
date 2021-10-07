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
	componentDeployDelay   = retry.BackoffDelay(200 * time.Millisecond)
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
	res := make([]string, 0)
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

func (i *operatorComponent) Ingresses() ingress.Instances {
	var out ingress.Instances
	for _, c := range i.ctx.Clusters().Kube() {
		// call IngressFor in-case initialization is needed.
		out = append(out, i.IngressFor(c))
	}
	return out
}

func (i *operatorComponent) IngressFor(c cluster.Cluster) ingress.Instance {
	return i.CustomIngressFor(c, defaultIngressServiceName, defaultIngressIstioLabel)
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

	defer func() {
		_ = f.Close()
	}()

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
		// Make sure to clean up primary clusters before remotes, or istiod will recreate some of the CMs that we delete
		// in the remote clusters before it's deleted.
		for _, c := range i.ctx.AllClusters().Primaries().Kube() {
			i.cleanupCluster(c, &errG)
		}
		for _, c := range i.ctx.Clusters().Remotes().Kube() {
			i.cleanupCluster(c, &errG)
		}
		return errG.Wait().ErrorOrNil()
	}
	return nil
}

func (i *operatorComponent) cleanupCluster(c cluster.Cluster, errG *multierror.Group) {
	scopes.Framework.Infof("clean up cluster %s", c.Name())
	errG.Go(func() (err error) {
		if e := i.ctx.Config(c).DeleteYAML("", removeCRDsSlice(i.installManifest[c.Name()])); e != nil {
			err = multierror.Append(err, e)
		}
		// Cleanup all secrets and configmaps - these are dynamically created by tests and/or istiod so they are not captured above
		// This includes things like leader election locks (allowing next test to start without 30s delay),
		// custom cacerts, custom kubeconfigs, etc.
		// We avoid deleting the whole namespace since its extremely slow in Kubernetes (30-60s+)
		if e := c.CoreV1().Secrets(i.settings.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.CoreV1().ConfigMaps(i.settings.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		// Delete validating and mutating webhook configurations. These can be created outside of generated manifests
		// when installing with istioctl and must be deleted separately.
		if e := c.AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.AdmissionregistrationV1().MutatingWebhookConfigurations().DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		return
	})
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
			err := os.WriteFile(path.Join(clusterDir, "manifest-"+strconv.Itoa(i)+".yaml"), []byte(manifest), 0o644)
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
	kube2.DumpPods(ctx, d, ns, []string{})
	kube2.DumpWebhooks(ctx, d)
	for _, c := range ctx.Clusters().Kube() {
		kube2.DumpDebug(ctx, c, d, "configz")
	}
	// Dump istio-cni.
	kube2.DumpPods(ctx, d, "kube-system", []string{"k8s-app=istio-cni-node"})
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
	if i.isExternalControlPlane() {
		cfg.PrimaryClusterIOPFile = IntegrationTestExternalIstiodPrimaryDefaultsIOP
		cfg.ConfigClusterIOPFile = IntegrationTestExternalIstiodConfigDefaultsIOP
		cfg.RemoteClusterIOPFile = IntegrationTestExternalIstiodRemoteDefaultsIOP
		i.settings = cfg
	} else if cfg.IstiodlessRemotes {
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

	// generate common IstioOperator yamls for different cluster types (primary, remote, remote-config)
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

	// First install remote-config clusters.
	// We do this first because the external istiod needs to read the config cluster at startup.
	for _, c := range ctx.Clusters().Kube().Configs().Remotes() {
		if err = installConfigCluster(i, cfg, c, istioctlConfigFiles.configIopFile); err != nil {
			return i, err
		}
	}

	// Install control plane clusters (can be external or primary).
	errG := multierror.Group{}
	for _, c := range ctx.AllClusters().Kube().Primaries() {
		c := c
		errG.Go(func() error {
			return installControlPlaneCluster(i, cfg, c, istioctlConfigFiles.iopFile, istioctlConfigFiles.operatorSpec)
		})
	}
	if err := errG.Wait(); err != nil {
		scopes.Framework.Errorf("one or more errors occurred installing control-plane clusters: %v", err)
		return i, err
	}

	if ctx.Clusters().IsMulticluster() && !i.isExternalControlPlane() {
		// For multicluster, configure direct access so each control plane can get endpoints from all API servers.
		// TODO: this should be done after installing the remote clusters, but needs to be done before for now,
		// because in non-external control plane MC, remote clusters are not really istiodless and they install
		// the gateways right away as part of default profile, which hangs if the control plane isn't responding.
		if err := i.configureDirectAPIServerAccess(ctx, cfg); err != nil {
			return nil, err
		}
	}

	// Install (non-config) remote clusters.
	errG = multierror.Group{}
	for _, c := range ctx.Clusters().Kube().Remotes(ctx.Clusters().Configs()...) {
		c := c
		errG.Go(func() error {
			if err := installRemoteCluster(i, cfg, c, istioctlConfigFiles.remoteIopFile); err != nil {
				return fmt.Errorf("failed installing remote cluster %s: %v", c.Name(), err)
			}
			return nil
		})
	}
	if errs := errG.Wait(); errs != nil {
		return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
	}

	if ctx.Clusters().IsMulticluster() && i.isExternalControlPlane() {
		// For multicluster, configure direct access so each control plane can get endpoints from all API servers.
		if err := i.configureDirectAPIServerAccess(ctx, cfg); err != nil {
			return nil, err
		}
	}

	// Configure discovery and gateways for remote clusters.
	for _, c := range ctx.Clusters().Kube().Remotes() {
		c := c
		if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
			if err = configureRemoteClusterDiscovery(i, cfg, c); err != nil {
				return i, err
			}

			// Install ingress and egress gateways
			// These need to be installed as a separate step for external control planes because config clusters are installed
			// before the external control plane cluster. Since remote clusters use gateway injection, we can't install the gateways
			// until after the control plane is running, so we install them here. This is not really necessary for pure (non-config)
			// remote clusters, but it's cleaner to just install gateways as a separate step for all remote clusters.
			if err = installRemoteClusterGateways(i, c); err != nil {
				return i, err
			}
		}

		// remote clusters only need east-west gateway for multi-network purposes
		if ctx.Environment().IsMultinetwork() {
			spec := istioctlConfigFiles.remoteOperatorSpec
			if c.IsConfig() {
				spec = istioctlConfigFiles.configOperatorSpec
			}
			if err := i.deployEastWestGateway(c, spec.Revision); err != nil {
				return i, err
			}

			// Wait for the eastwestgateway to have a public IP.
			_ = i.CustomIngressFor(c, eastWestIngressServiceName, eastWestIngressIstioLabel).DiscoveryAddress()
		}
	}

	if env.IsMultinetwork() {
		// enable cross network traffic
		for _, c := range ctx.Clusters().Kube().Configs() {
			if err := i.exposeUserServices(c); err != nil {
				return nil, err
			}
		}
	}

	return i, nil
}

// patchIstiodCustomHost sets the ISTIOD_CUSTOM_HOST to the given address,
// to allow webhook connections to succeed when reaching webhook by IP.
func patchIstiodCustomHost(istiodAddress net.TCPAddr, cfg Config, c cluster.Cluster) error {
	scopes.Framework.Infof("patching custom host for cluster %s as %s", c.Name(), istiodAddress.IP.String())
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
	if _, err := c.AppsV1().Deployments(cfg.ConfigNamespace).Patch(context.TODO(), "istiod", types.ApplyPatchType,
		[]byte(contents), patchOptions); err != nil {
		return fmt.Errorf("failed to patch istiod with ISTIOD_CUSTOM_HOST: %v", err)
	}

	if err := retry.UntilSuccess(func() error {
		pods, err := c.CoreV1().Pods(cfg.SystemNamespace).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: "app=istiod"})
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
		return fmt.Errorf("failed waiting for patched istiod pod to come up in %s: %v", c.Name(), err)
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

	if err := os.WriteFile(iopFile, []byte(out), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write iop: %v", err)
	}

	return operatorCfg.Spec, nil
}

// installControlPlaneCluster installs the istiod control plane to the given cluster.
// The cluster is considered a "primary" cluster if it is also a "config cluster", in which case components
// like ingress will be installed.
func installControlPlaneCluster(i *operatorComponent, cfg Config, c cluster.Cluster, iopFile string,
	spec *opAPI.IstioOperatorSpec) error {
	scopes.Framework.Infof("setting up %s as control-plane cluster", c.Name())

	if !c.IsConfig() {
		if err := i.configureRemoteConfigForControlPlane(c); err != nil {
			return err
		}
	}
	installSettings, err := i.generateCommonInstallSettings(cfg, c, cfg.PrimaryClusterIOPFile, iopFile)
	if err != nil {
		return err
	}

	// Set the clusterName for the local cluster.
	// This MUST match the clusterName in the remote secret for this cluster.
	if i.environment.IsMulticluster() {
		if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
			// enable namespace controller writing to remote clusters
			installSettings = append(installSettings, "--set", "values.pilot.env.EXTERNAL_ISTIOD=true")
		}
		clusterName := c.Name()
		if !c.IsConfig() {
			clusterName = c.ConfigName()
		}
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+clusterName)
	}
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: c,
	})
	if err != nil {
		return err
	}

	err = install(i, installSettings, istioCtl, c.Name())
	if err != nil {
		return err
	}

	if c.IsConfig() {
		// this is a traditional primary cluster, install the eastwest gateway

		// there are a few tests that require special gateway setup which will cause eastwest gateway fail to start
		// exclude these tests from installing eastwest gw for now
		if !cfg.DeployEastWestGW {
			return nil
		}

		if err := i.deployEastWestGateway(c, spec.Revision); err != nil {
			return err
		}
		// Other clusters should only use this for discovery if its a config cluster.
		if err := i.applyIstiodGateway(c, spec.Revision); err != nil {
			return fmt.Errorf("failed applying istiod gateway for cluster %s: %v", c.Name(), err)
		}
		if err := waitForIstioReady(i.ctx, c, cfg); err != nil {
			return err
		}
	}

	if !c.IsConfig() || settingsFromCommandline.IstiodlessRemotes {
		// patch the ISTIOD_CUSTOM_HOST to allow using the webhook by IP (this is something we only do in tests)

		// fetch after installing eastwest (for external istiod, this will be loadbalancer address of istiod directly)
		istiodAddress, err := i.RemoteDiscoveryAddressFor(c)
		if err != nil {
			return err
		}

		// TODO generate & install a valid cert in CI
		if err := patchIstiodCustomHost(istiodAddress, cfg, c); err != nil {
			return err
		}
	}

	return nil
}

// installConfigCluster installs istio to a cluster that runs workloads and provides Istio configuration.
// The installed components include CRDs, Roles, etc. but not istiod.
func installConfigCluster(i *operatorComponent, cfg Config, c cluster.Cluster, configIopFile string) error {
	scopes.Framework.Infof("setting up %s as config cluster", c.Name())
	return installRemoteCommon(i, cfg, c, cfg.ConfigClusterIOPFile, configIopFile)
}

// installRemoteCluster installs istio to a remote cluster that does not also serve as a config cluster.
func installRemoteCluster(i *operatorComponent, cfg Config, c cluster.Cluster, remoteIopFile string) error {
	scopes.Framework.Infof("setting up %s as remote cluster", c.Name())
	return installRemoteCommon(i, cfg, c, cfg.RemoteClusterIOPFile, remoteIopFile)
}

// Common install on a either a remote-config or pure remote cluster.
func installRemoteCommon(i *operatorComponent, cfg Config, c cluster.Cluster, defaultsIOPFile, iopFile string) error {
	installSettings, err := i.generateCommonInstallSettings(cfg, c, defaultsIOPFile, iopFile)
	if err != nil {
		return err
	}
	if i.environment.IsMulticluster() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installSettings = append(installSettings, "--set", "values.global.multiCluster.clusterName="+c.Name())
	}
	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: c,
	})
	if err != nil {
		return err
	}

	// Configure the cluster and network arguments to pass through the injector webhook.
	if i.isExternalControlPlane() {
		installSettings = append(installSettings,
			"--set", fmt.Sprintf("values.istiodRemote.injectionPath=/inject/net/%s/cluster/%s", c.NetworkName(), c.Name()))
	} else {
		remoteIstiodAddress, err := i.RemoteDiscoveryAddressFor(c)
		if err != nil {
			return err
		}
		installSettings = append(installSettings, "--set", "values.global.remotePilotAddress="+remoteIstiodAddress.IP.String())
		if cfg.IstiodlessRemotes {
			installSettings = append(installSettings,
				"--set", fmt.Sprintf("values.istiodRemote.injectionURL=https://%s:%d/inject/net/%s/cluster/%s",
					remoteIstiodAddress.IP.String(), 15017, c.NetworkName(), c.Name()),
				"--set", fmt.Sprintf("values.base.validationURL=https://%s:%d/validate", remoteIstiodAddress.IP.String(), 15017))
		}
	}

	if err := install(i, installSettings, istioCtl, c.Name()); err != nil {
		return err
	}

	return nil
}

func installRemoteClusterGateways(i *operatorComponent, c cluster.Cluster) error {
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return err
	}
	installSettings := []string{
		"-f", filepath.Join(testenv.IstioSrc, IntegrationTestExternalIstiodRemoteGatewaysIOP),
		"--istioNamespace", i.settings.SystemNamespace,
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
		"--set", "values.global.imagePullPolicy=" + s.PullPolicy,
	}

	// Create an istioctl to configure this cluster.
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{
		Cluster: c,
	})
	if err != nil {
		return err
	}

	scopes.Framework.Infof("Deploying ingress and egress gateways in %s: %v", c.Name(), installSettings)
	if err = install(i, installSettings, istioCtl, c.Name()); err != nil {
		return err
	}

	return nil
}

func (i *operatorComponent) generateCommonInstallSettings(cfg Config, c cluster.Cluster, defaultsIOPFile, iopFile string) ([]string, error) {
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

	if i.environment.IsMultinetwork() && c.NetworkName() != "" {
		installSettings = append(installSettings,
			"--set", "values.global.meshID="+meshID,
			"--set", "values.global.network="+c.NetworkName())
	}

	// Include all user-specified values and configuration options.
	if cfg.EnableCNI {
		installSettings = append(installSettings,
			"--set", "components.cni.namespace=kube-system",
			"--set", "components.cni.enabled=true")
	}

	// Include all user-specified values.
	for k, v := range cfg.Values {
		installSettings = append(installSettings, "--set", fmt.Sprintf("values.%s=%s", k, v))
	}

	for k, v := range cfg.OperatorOptions {
		installSettings = append(installSettings, "--set", fmt.Sprintf("%s=%s", k, v))
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

func waitForIstioReady(ctx resource.Context, c cluster.Cluster, cfg Config) error {
	if !cfg.SkipWaitForValidationWebhook {
		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		if err := waitForValidationWebhook(ctx, c, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServerAccess(ctx resource.Context, cfg Config) error {
	// Configure direct access for each control plane to each APIServer. This allows each control plane to
	// automatically discover endpoints in remote clusters.
	for _, c := range ctx.Clusters().Kube() {
		if err := i.configureDirectAPIServiceAccessForCluster(ctx, cfg, c); err != nil {
			return err
		}
	}
	return nil
}

func (i *operatorComponent) configureDirectAPIServiceAccessForCluster(ctx resource.Context, cfg Config,
	c cluster.Cluster) error {
	clusters := ctx.Clusters().Configs(c)
	if len(clusters) == 0 {
		// giving 0 clusters to ctx.Config() means using all clusters
		return nil
	}
	// Create a secret.
	secret, err := CreateRemoteSecret(ctx, c, cfg)
	if err != nil {
		return fmt.Errorf("failed creating remote secret for cluster %s: %v", c.Name(), err)
	}
	if err := ctx.Config(clusters...).ApplyYAMLNoCleanup(cfg.SystemNamespace, secret); err != nil {
		return fmt.Errorf("failed applying remote secret to clusters: %v", err)
	}
	return nil
}

func CreateRemoteSecret(ctx resource.Context, c cluster.Cluster, cfg Config, opts ...string) (string, error) {
	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: c,
	})
	if err != nil {
		return "", err
	}
	cmd := []string{
		"create-remote-secret",
		"--name", c.Name(),
		"--namespace", cfg.SystemNamespace,
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
	}
	cmd = append(cmd, opts...)

	scopes.Framework.Infof("Creating remote secret for cluster cluster %s %v", c.Name(), cmd)
	out, _, err := istioCtl.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("create remote secret failed for cluster %s: %v", c.Name(), err)
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

	for _, c := range env.Clusters() {
		// Create a subdir for the cluster certs.
		clusterDir := filepath.Join(certsDir, c.Name())
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
			return fmt.Errorf("failed creating intermediate CA for cluster %s: %v", c.Name(), err)
		}

		// Create the CA secret for this cluster. Istio will use these certs for its CA rather
		// than its autogenerated self-signed root.
		secret, err := clusterCA.NewIstioCASecret()
		if err != nil {
			return fmt.Errorf("failed creating intermediate CA secret for cluster %s: %v", c.Name(), err)
		}

		// Create the system namespace.
		var nsLabels map[string]string
		if env.IsMultinetwork() {
			nsLabels = map[string]string{label.TopologyNetwork.Name: c.NetworkName()}
		}
		if _, err := c.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Labels: nsLabels,
				Name:   cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := c.CoreV1().Namespaces().Update(context.TODO(), &kubeApiCore.Namespace{
					ObjectMeta: kubeApiMeta.ObjectMeta{
						Labels: nsLabels,
						Name:   cfg.SystemNamespace,
					},
				}, kubeApiMeta.UpdateOptions{}); err != nil {
					scopes.Framework.Errorf("failed updating namespace %s on cluster %s. This can happen when deploying "+
						"multiple control planes. Error: %v", cfg.SystemNamespace, c.Name(), err)
				}
			} else {
				scopes.Framework.Errorf("failed creating namespace %s on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", cfg.SystemNamespace, c.Name(), err)
			}
		}

		// Create the secret for the cacerts.
		if _, err := c.CoreV1().Secrets(cfg.SystemNamespace).Create(context.TODO(), secret,
			kubeApiMeta.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := c.CoreV1().Secrets(cfg.SystemNamespace).Update(context.TODO(), secret,
					kubeApiMeta.UpdateOptions{}); err != nil {
					scopes.Framework.Errorf("failed to create CA secrets on cluster %s. This can happen when deploying "+
						"multiple control planes. Error: %v", c.Name(), err)
				}
			} else {
				scopes.Framework.Errorf("failed to create CA secrets on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", c.Name(), err)
			}
		}
	}
	return nil
}

// configureRemoteClusterDiscovery creates a local istiod Service and Endpoints pointing to the external control plane.
// This is used to configure the remote cluster webhooks in the test environment.
// In a production deployment, the external istiod would be configured using proper DNS+certs instead.
func configureRemoteClusterDiscovery(i *operatorComponent, cfg Config, c cluster.Cluster) error {
	discoveryAddress, err := i.RemoteDiscoveryAddressFor(c)
	if err != nil {
		return err
	}
	discoveryIP := discoveryAddress.IP.String()

	scopes.Framework.Infof("creating endpoints and service in %s to get discovery from %s", c.Name(), discoveryIP)
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
					Port:     443,
				},
				{
					Name:     "tls",
					Protocol: kubeApiCore.ProtocolTCP,
					Port:     15443,
				},
			},
		},
	}
	if _, err = c.CoreV1().Services(cfg.SystemNamespace).Create(context.TODO(), svc, kubeApiMeta.CreateOptions{}); err != nil {
		// Ignore if service already exists. An update requires additional metadata.
		if !errors.IsAlreadyExists(err) {
			scopes.Framework.Errorf("failed to create services: %v", err)
			return err
		}
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
						IP: discoveryIP,
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
						Port:     443,
					},
				},
			},
		},
	}

	if _, err = c.CoreV1().Endpoints(cfg.SystemNamespace).Create(context.TODO(), eps, kubeApiMeta.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			if _, err = c.CoreV1().Endpoints(cfg.SystemNamespace).Update(context.TODO(), eps, kubeApiMeta.UpdateOptions{}); err != nil {
				scopes.Framework.Errorf("failed to update endpoints: %v", err)
				return err
			}
		} else {
			scopes.Framework.Errorf("failed to create endpoints: %v", err)
			return err
		}
	}

	err = retry.UntilSuccess(func() error {
		_, err := c.CoreV1().Services(cfg.SystemNamespace).Get(context.TODO(), istiodSvcName, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		_, err = c.CoreV1().Endpoints(cfg.SystemNamespace).Get(context.TODO(), istiodSvcName, kubeApiMeta.GetOptions{})
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
func (i *operatorComponent) configureRemoteConfigForControlPlane(c cluster.Cluster) error {
	cfg := i.settings
	configCluster := c.Config()
	istioKubeConfig, err := file.AsString(configCluster.(*kubecluster.Cluster).Filename())
	if err != nil {
		scopes.Framework.Infof("error in parsing kubeconfig for %s", configCluster.Name())
		return err
	}

	scopes.Framework.Infof("configuring external control plane in %s to use config cluster %s", c.Name(), configCluster.Name())
	// ensure system namespace exists
	if _, err = c.CoreV1().Namespaces().
		Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name: cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	// create kubeconfig secret
	if _, err = c.CoreV1().Secrets(cfg.SystemNamespace).
		Create(context.TODO(), &kubeApiCore.Secret{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:      "istio-kubeconfig",
				Namespace: cfg.SystemNamespace,
			},
			Data: map[string][]byte{
				"config": []byte(istioKubeConfig),
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) { // Allow easier running locally when we run multiple tests in a row
			if _, err := c.CoreV1().Secrets(cfg.SystemNamespace).Update(context.TODO(), &kubeApiCore.Secret{
				ObjectMeta: kubeApiMeta.ObjectMeta{
					Name:      "istio-kubeconfig",
					Namespace: cfg.SystemNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(istioKubeConfig),
				},
			}, kubeApiMeta.UpdateOptions{}); err != nil {
				scopes.Framework.Infof("error updating istio-kubeconfig secret: %v", err)
				return err
			}
		} else {
			scopes.Framework.Infof("error creating istio-kubeconfig secret %v", err)
			return err
		}
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

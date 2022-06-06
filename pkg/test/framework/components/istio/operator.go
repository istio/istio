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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	opAPI "istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/operator/cmd/mesh"
	pkgAPI "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/cert/ca"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/pkg/log"
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
	ingress map[string]map[string]ingress.Instance
	workDir string
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
		if r.MatchString(p) {
			continue
		}
		nonCrds = append(nonCrds, p)
	}

	return yml.JoinString(nonCrds...)
}

type istioctlConfigFiles struct {
	iopFile             string
	operatorSpec        *opAPI.IstioOperatorSpec
	configIopFile       string
	configOperatorSpec  *opAPI.IstioOperatorSpec
	remoteIopFile       string
	remoteOperatorSpec  *opAPI.IstioOperatorSpec
	eastWestGatewayFile string
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

func (i *operatorComponent) EastWestGatewayFor(c cluster.Cluster) ingress.Instance {
	return i.CustomIngressFor(c, eastWestIngressServiceName, eastWestIngressIstioLabel)
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
		ingr := newIngress(i.ctx, ingressConfig{
			Namespace:   i.settings.SystemNamespace,
			Cluster:     c,
			ServiceName: serviceName,
			IstioLabel:  istioLabel,
		})
		if closer, ok := ingr.(io.Closer); ok {
			i.ctx.Cleanup(func() { _ = closer.Close() })
		}
		i.ingress[c.Name()][istioLabel] = ingr
	}
	return i.ingress[c.Name()][istioLabel]
}

func (i *operatorComponent) Close() error {
	t0 := time.Now()
	scopes.Framework.Infof("=== BEGIN: Cleanup Istio [Suite=%s] ===", i.ctx.Settings().TestID)

	// Write time spent for cleanup and deploy to ARTIFACTS/trace.yaml and logs to allow analyzing test times
	defer func() {
		delta := time.Since(t0)
		i.ctx.RecordTraceEvent("istio-cleanup", delta.Seconds())
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
		if e := i.ctx.ConfigKube(c).YAML("", removeCRDsSlice(i.installManifest[c.Name()])).Delete(); e != nil {
			err = multierror.Append(err, e)
		}
		// Cleanup all secrets and configmaps - these are dynamically created by tests and/or istiod so they are not captured above
		// This includes things like leader election locks (allowing next test to start without 30s delay),
		// custom cacerts, custom kubeconfigs, etc.
		// We avoid deleting the whole namespace since its extremely slow in Kubernetes (30-60s+)
		if e := c.Kube().CoreV1().Secrets(i.settings.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().CoreV1().ConfigMaps(i.settings.SystemNamespace).DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		// Delete validating and mutating webhook configurations. These can be created outside of generated manifests
		// when installing with istioctl and must be deleted separately.
		if e := c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(
			context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
			err = multierror.Append(err, e)
		}
		if e := c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().DeleteCollection(
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
	for _, c := range ctx.Clusters().Kube().Primaries() {
		kube2.DumpDebug(ctx, c, d, "configz")
		kube2.DumpDebug(ctx, c, d, "mcsz")
		kube2.DumpDebug(ctx, c, d, "clusterz")
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
		i.settings = cfg
	} else if !cfg.IstiodlessRemotes {
		cfg.RemoteClusterIOPFile = IntegrationTestDefaultsIOP
		i.settings = cfg
	}

	scopes.Framework.Infof("=== Istio Component Config ===")
	scopes.Framework.Infof("\n%s", cfg.String())
	scopes.Framework.Infof("================================")

	t0 := time.Now()
	defer func() {
		ctx.RecordTraceEvent("istio-deploy", time.Since(t0).Seconds())
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
	istioctlConfigFiles, err := createIstioctlConfigFile(ctx.Settings(), workDir, cfg)
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
	s := ctx.Settings()
	for _, c := range ctx.Clusters().Kube().Configs().Remotes() {
		if err = installConfigCluster(s, i, cfg, c, istioctlConfigFiles.configIopFile); err != nil {
			return i, err
		}
	}

	// Install control plane clusters (can be external or primary).
	errG := multierror.Group{}
	for _, c := range ctx.AllClusters().Kube().Primaries() {
		c := c
		errG.Go(func() error {
			return installControlPlaneCluster(s, i, cfg, c, istioctlConfigFiles.iopFile, istioctlConfigFiles.operatorSpec, istioctlConfigFiles.eastWestGatewayFile)
		})
	}
	if err := errG.Wait().ErrorOrNil(); err != nil {
		scopes.Framework.Errorf("one or more errors occurred installing control-plane clusters: %v", err)
		return i, err
	}

	// Update config clusters now that external istiod is running.
	for _, c := range ctx.Clusters().Kube().Configs().Remotes() {
		if err = reinstallConfigCluster(s, i, cfg, c, istioctlConfigFiles.configIopFile); err != nil {
			return i, err
		}
	}

	// Install (non-config) remote clusters.
	errG = multierror.Group{}
	for _, c := range ctx.Clusters().Kube().Remotes(ctx.Clusters().Configs()...) {
		c := c
		errG.Go(func() error {
			// Configure API server access for the remote cluster's primary cluster control plane.
			if err := i.configureDirectAPIServiceAccessBetweenClusters(ctx, cfg, c, c.Config()); err != nil {
				return fmt.Errorf("failed providing primary cluster access for remote cluster %s: %v", c.Name(), err)
			}
			if err := installRemoteCluster(s, i, cfg, c, istioctlConfigFiles.remoteIopFile); err != nil {
				return fmt.Errorf("failed installing remote cluster %s: %v", c.Name(), err)
			}
			return nil
		})
	}
	if errs := errG.Wait(); errs != nil {
		return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
	}

	// For multicluster, configure direct access so each control plane can get endpoints from all API servers.
	if ctx.Clusters().IsMulticluster() {
		if err := i.configureDirectAPIServerAccess(ctx, cfg); err != nil {
			return nil, err
		}
	}

	// Configure gateways for remote clusters.
	for _, c := range ctx.Clusters().Kube().Remotes() {
		c := c
		if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
			// Install ingress and egress gateways
			// These need to be installed as a separate step for external control planes because config clusters are installed
			// before the external control plane cluster. Since remote clusters use gateway injection, we can't install the gateways
			// until after the control plane is running, so we install them here. This is not really necessary for pure (non-config)
			// remote clusters, but it's cleaner to just install gateways as a separate step for all remote clusters.
			if err = installRemoteClusterGateways(s, i, c); err != nil {
				return i, err
			}
		}

		// remote clusters only need east-west gateway for multi-network purposes
		if ctx.Environment().IsMultinetwork() {
			spec := istioctlConfigFiles.remoteOperatorSpec
			if c.IsConfig() {
				spec = istioctlConfigFiles.configOperatorSpec
			}
			if err := i.deployEastWestGateway(c, spec.Revision, istioctlConfigFiles.eastWestGatewayFile); err != nil {
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

func initIOPFile(s *resource.Settings, cfg Config, iopFile string, valuesYaml string) (*opAPI.IstioOperatorSpec, error) {
	operatorYaml := cfg.IstioOperatorConfigYAML(s, valuesYaml)

	operatorCfg := &pkgAPI.IstioOperator{}
	if err := yaml.Unmarshal([]byte(operatorYaml), operatorCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base iop: %v, %v", err, operatorYaml)
	}

	// marshaling entire operatorCfg causes panic because of *time.Time in ObjectMeta
	outb, err := yaml.Marshal(operatorCfg.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling iop spec: %v", err)
	}

	out := fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
%s`, Indent(string(outb), "  "))

	if err := os.WriteFile(iopFile, []byte(out), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write iop: %v", err)
	}

	return operatorCfg.Spec, nil
}

// installControlPlaneCluster installs the istiod control plane to the given cluster.
// The cluster is considered a "primary" cluster if it is also a "config cluster", in which case components
// like ingress will be installed.
func installControlPlaneCluster(s *resource.Settings, i *operatorComponent, cfg Config, c cluster.Cluster, iopFile string,
	spec *opAPI.IstioOperatorSpec, eastwestSettings string,
) error {
	scopes.Framework.Infof("setting up %s as control-plane cluster", c.Name())

	if !c.IsConfig() {
		if err := i.configureRemoteConfigForControlPlane(c); err != nil {
			return err
		}
	}
	installArgs, err := i.generateCommonInstallArgs(s, cfg, c, cfg.PrimaryClusterIOPFile, iopFile)
	if err != nil {
		return err
	}

	if i.environment.IsMulticluster() {
		if !i.isExternalControlPlane() && !cfg.IstiodlessRemotes {
			// Disable namespace controller writing to remote clusters
			installArgs.Set = append(installArgs.Set, "values.pilot.env.EXTERNAL_ISTIOD=false")
		}

		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		clusterName := c.Name()
		if !c.IsConfig() {
			clusterName = c.ConfigName()
		}
		installArgs.Set = append(installArgs.Set, "values.global.multiCluster.clusterName="+clusterName)
	}

	err = install(i, installArgs, c.Name())
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

		if err := i.deployEastWestGateway(c, spec.Revision, eastwestSettings); err != nil {
			return err
		}
		// Other clusters should only use this for discovery if its a config cluster.
		if err := i.applyIstiodGateway(c, spec.Revision); err != nil {
			return fmt.Errorf("failed applying istiod gateway for cluster %s: %v", c.Name(), err)
		}
		if err := waitForIstioReady(i.ctx, c, cfg); err != nil {
			return err
		}
	} else {
		// configure istioctl to run with an external control plane topology.
		istiodAddress, err := i.RemoteDiscoveryAddressFor(c)
		if err != nil {
			return err
		}
		_ = os.Setenv("ISTIOCTL_XDS_ADDRESS", istiodAddress.String())
		_ = os.Setenv("ISTIOCTL_PREFER_EXPERIMENTAL", "true")
		if err := cmd.ConfigAndEnvProcessing(); err != nil {
			return err
		}
	}

	return nil
}

// reinstallConfigCluster updates the config cluster installation after the external discovery address is available.
func reinstallConfigCluster(s *resource.Settings, i *operatorComponent, cfg Config, c cluster.Cluster, configIopFile string) error {
	scopes.Framework.Infof("updating setup for config cluster %s", c.Name())
	return installRemoteCommon(s, i, cfg, c, cfg.ConfigClusterIOPFile, configIopFile, true)
}

// installConfigCluster installs istio to a cluster that runs workloads and provides Istio configuration.
// The installed components include CRDs, Roles, etc. but not istiod.
func installConfigCluster(s *resource.Settings, i *operatorComponent, cfg Config, c cluster.Cluster, configIopFile string) error {
	scopes.Framework.Infof("setting up %s as config cluster", c.Name())
	return installRemoteCommon(s, i, cfg, c, cfg.ConfigClusterIOPFile, configIopFile, false)
}

// installRemoteCluster installs istio to a remote cluster that does not also serve as a config cluster.
func installRemoteCluster(s *resource.Settings, i *operatorComponent, cfg Config, c cluster.Cluster, remoteIopFile string) error {
	scopes.Framework.Infof("setting up %s as remote cluster", c.Name())
	return installRemoteCommon(s, i, cfg, c, cfg.RemoteClusterIOPFile, remoteIopFile, true)
}

// Common install on a either a remote-config or pure remote cluster.
func installRemoteCommon(s *resource.Settings, i *operatorComponent, cfg Config, c cluster.Cluster, defaultsIOPFile, iopFile string, discovery bool) error {
	installArgs, err := i.generateCommonInstallArgs(s, cfg, c, defaultsIOPFile, iopFile)
	if err != nil {
		return err
	}
	if i.environment.IsMulticluster() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		installArgs.Set = append(installArgs.Set, "values.global.multiCluster.clusterName="+c.Name())
	}

	if discovery {
		// Configure the cluster and network arguments to pass through the injector webhook.
		remoteIstiodAddress, err := i.RemoteDiscoveryAddressFor(c)
		if err != nil {
			return err
		}
		installArgs.Set = append(installArgs.Set, "values.global.remotePilotAddress="+remoteIstiodAddress.IP.String())
	}

	if i.isExternalControlPlane() || cfg.IstiodlessRemotes {
		installArgs.Set = append(installArgs.Set,
			fmt.Sprintf("values.istiodRemote.injectionPath=/inject/net/%s/cluster/%s", c.NetworkName(), c.Name()))
	}

	if err := install(i, installArgs, c.Name()); err != nil {
		return err
	}

	return nil
}

func installRemoteClusterGateways(s *resource.Settings, i *operatorComponent, c cluster.Cluster) error {
	kubeConfigFile, err := kubeConfigFileForCluster(c)
	if err != nil {
		return err
	}

	installArgs := &mesh.InstallArgs{
		KubeConfigPath: kubeConfigFile,
		ManifestsPath:  filepath.Join(testenv.IstioSrc, "manifests"),
		InFilenames: []string{
			filepath.Join(testenv.IstioSrc, IntegrationTestRemoteGatewaysIOP),
		},
		Set: []string{
			"values.global.imagePullPolicy=" + s.Image.PullPolicy,
		},
	}

	scopes.Framework.Infof("Deploying ingress and egress gateways in %s: %v", c.Name(), installArgs)
	if err = install(i, installArgs, c.Name()); err != nil {
		return err
	}

	return nil
}

func kubeConfigFileForCluster(c cluster.Cluster) (string, error) {
	type Filenamer interface {
		Filename() string
	}
	fn, ok := c.(Filenamer)
	if !ok {
		return "", fmt.Errorf("cluster does not support fetching kube config")
	}
	return fn.Filename(), nil
}

func (i *operatorComponent) generateCommonInstallArgs(s *resource.Settings, cfg Config, c cluster.Cluster, defaultsIOPFile,
	iopFile string,
) (*mesh.InstallArgs, error) {
	kubeConfigFile, err := kubeConfigFileForCluster(c)
	if err != nil {
		return nil, err
	}

	if !path.IsAbs(defaultsIOPFile) {
		defaultsIOPFile = filepath.Join(testenv.IstioSrc, defaultsIOPFile)
	}
	baseIOP := cfg.BaseIOPFile
	if !path.IsAbs(baseIOP) {
		baseIOP = filepath.Join(testenv.IstioSrc, baseIOP)
	}

	installArgs := &mesh.InstallArgs{
		KubeConfigPath: kubeConfigFile,
		ManifestsPath:  filepath.Join(testenv.IstioSrc, "manifests"),
		InFilenames: []string{
			baseIOP,
			defaultsIOPFile,
			iopFile,
		},
		Set: []string{
			"values.global.imagePullPolicy=" + s.Image.PullPolicy,
		},
	}

	if i.environment.IsMultinetwork() && c.NetworkName() != "" {
		installArgs.Set = append(installArgs.Set,
			"values.global.meshID="+meshID,
			"values.global.network="+c.NetworkName())
	}

	// Include all user-specified values and configuration options.
	if cfg.EnableCNI {
		installArgs.Set = append(installArgs.Set,
			"components.cni.namespace=kube-system",
			"components.cni.enabled=true")
	}

	// Include all user-specified values.
	for k, v := range cfg.Values {
		installArgs.Set = append(installArgs.Set, fmt.Sprintf("values.%s=%s", k, v))
	}

	for k, v := range cfg.OperatorOptions {
		installArgs.Set = append(installArgs.Set, fmt.Sprintf("%s=%s", k, v))
	}
	return installArgs, nil
}

// install will replace and reconcile the installation based on the given install settings
func install(c *operatorComponent, installArgs *mesh.InstallArgs, clusterName string) error {
	var stdOut, stdErr bytes.Buffer
	if err := mesh.ManifestGenerate(&mesh.RootArgs{}, &mesh.ManifestGenerateArgs{
		InFilenames:   installArgs.InFilenames,
		Set:           installArgs.Set,
		Force:         installArgs.Force,
		ManifestsPath: installArgs.ManifestsPath,
		Revision:      installArgs.Revision,
	}, cmdLogOptions(), cmdLogger(&stdOut, &stdErr)); err != nil {
		return err
	}
	c.saveManifestForCleanup(clusterName, stdOut.String())

	// Actually run the install command
	installArgs.SkipConfirmation = true

	scopes.Framework.Infof("Installing Istio components on cluster %s %s", clusterName, installArgs)
	stdOut.Reset()
	stdErr.Reset()
	if err := mesh.Install(&mesh.RootArgs{}, installArgs, cmdLogOptions(), &stdOut,
		cmdLogger(&stdOut, &stdErr),
		mesh.NewPrinterForWriter(&stdOut)); err != nil {
		return fmt.Errorf("install failed: %v: %s", err, &stdErr)
	}
	return nil
}

func cmdLogOptions() *log.Options {
	o := log.DefaultOptions()

	// These scopes are, at the default "INFO" level, too chatty for command line use
	o.SetOutputLevel("validation", log.ErrorLevel)
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("analysis", log.WarnLevel)
	o.SetOutputLevel("installer", log.WarnLevel)
	o.SetOutputLevel("translator", log.WarnLevel)
	o.SetOutputLevel("adsc", log.WarnLevel)
	o.SetOutputLevel("default", log.WarnLevel)
	o.SetOutputLevel("klog", log.WarnLevel)
	o.SetOutputLevel("kube", log.ErrorLevel)

	return o
}

func cmdLogger(stdOut, stdErr io.Writer) clog.Logger {
	return clog.NewConsoleLogger(stdOut, stdErr, scopes.Framework)
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
	c cluster.Cluster,
) error {
	clusters := ctx.Clusters().Configs(c.Config())
	if len(clusters) == 0 {
		// giving 0 clusters to ctx.ConfigKube() means using all clusters
		return nil
	}
	return i.configureDirectAPIServiceAccessBetweenClusters(ctx, cfg, c, clusters...)
}

func (i *operatorComponent) configureDirectAPIServiceAccessBetweenClusters(ctx resource.Context, cfg Config,
	c cluster.Cluster, from ...cluster.Cluster,
) error {
	// Create a secret.
	secret, err := CreateRemoteSecret(ctx, c, cfg)
	if err != nil {
		return fmt.Errorf("failed creating remote secret for cluster %s: %v", c.Name(), err)
	}
	if err := ctx.ConfigKube(from...).
		YAML(cfg.SystemNamespace, secret).
		Apply(apply.NoCleanup); err != nil {
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

	scopes.Framework.Infof("Creating remote secret for cluster %s %v", c.Name(), cmd)
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
		if _, err := c.Kube().CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Labels: nsLabels,
				Name:   cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := c.Kube().CoreV1().Namespaces().Update(context.TODO(), &kubeApiCore.Namespace{
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
		if _, err := c.Kube().CoreV1().Secrets(cfg.SystemNamespace).Create(context.TODO(), secret,
			kubeApiMeta.CreateOptions{}); err != nil {
			// no need to do anything if cacerts is already present
			if !errors.IsAlreadyExists(err) {
				scopes.Framework.Errorf("failed to create CA secrets on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", c.Name(), err)
			}
		}
	}
	return nil
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
	if _, err = c.Kube().CoreV1().Namespaces().
		Create(context.TODO(), &kubeApiCore.Namespace{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name: cfg.SystemNamespace,
			},
		}, kubeApiMeta.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	// create kubeconfig secret
	if _, err = c.Kube().CoreV1().Secrets(cfg.SystemNamespace).
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
			if _, err := c.Kube().CoreV1().Secrets(cfg.SystemNamespace).Update(context.TODO(), &kubeApiCore.Secret{
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

func createIstioctlConfigFile(s *resource.Settings, workDir string, cfg Config) (istioctlConfigFiles, error) {
	var err error
	configFiles := istioctlConfigFiles{
		iopFile:             "",
		configIopFile:       "",
		remoteIopFile:       "",
		eastWestGatewayFile: "",
	}
	// Generate the istioctl config file for control plane(primary) cluster
	configFiles.iopFile = filepath.Join(workDir, "iop.yaml")
	if configFiles.operatorSpec, err = initIOPFile(s, cfg, configFiles.iopFile, cfg.ControlPlaneValues); err != nil {
		return configFiles, err
	}

	// Generate the istioctl config file for remote cluster
	if cfg.RemoteClusterValues == "" {
		cfg.RemoteClusterValues = cfg.ControlPlaneValues
	}

	configFiles.remoteIopFile = filepath.Join(workDir, "remote.yaml")
	if configFiles.remoteOperatorSpec, err = initIOPFile(s, cfg, configFiles.remoteIopFile, cfg.RemoteClusterValues); err != nil {
		return configFiles, err
	}

	// Generate the istioctl config file for config cluster
	configFiles.configIopFile = configFiles.iopFile
	configFiles.configOperatorSpec = configFiles.operatorSpec
	if cfg.ConfigClusterValues != "" {
		configFiles.configIopFile = filepath.Join(workDir, "config.yaml")
		if configFiles.configOperatorSpec, err = initIOPFile(s, cfg, configFiles.configIopFile, cfg.ConfigClusterValues); err != nil {
			return configFiles, err
		}
	}

	if cfg.EastWestGatewayValues != "" {
		configFiles.eastWestGatewayFile = filepath.Join(workDir, "eastwest.yaml")
		_, err = initIOPFile(s, cfg, configFiles.eastWestGatewayFile, cfg.EastWestGatewayValues)
		if err != nil {
			return configFiles, err
		}
	}
	return configFiles, nil
}

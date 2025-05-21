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
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/istioctl/cmd"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/values"
	istiokube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/cert/ca"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/istiomultierror"
)

// TODO: dynamically generate meshID to support multi-tenancy tests
const (
	meshID        = "testmesh0"
	istiodSvcName = "istiod"
	istiodLabel   = "pilot"
)

var (
	// the retry options for waiting for an individual component to be ready
	componentDeployTimeout = retry.Timeout(1 * time.Minute)
	componentDeployDelay   = retry.BackoffDelay(200 * time.Millisecond)

	_ io.Closer       = &istioImpl{}
	_ Instance        = &istioImpl{}
	_ resource.Dumper = &istioImpl{}
)

type istioImpl struct {
	id                   resource.ID
	cfg                  Config
	ctx                  resource.Context
	env                  *kube.Environment
	externalControlPlane bool
	installer            *installer
	*meshConfig
	injectConfig *injectConfig

	mu sync.Mutex
	// ingress components, indexed first by cluster name and then by gateway name.
	ingress map[string]map[string]ingress.Instance
	istiod  map[string]istiokube.PortForwarder
	workDir string
	iopFiles
}

type iopInfo struct {
	file string
	spec *iopv1alpha1.IstioOperatorSpec
}

type iopFiles struct {
	primaryIOP  iopInfo
	configIOP   iopInfo
	remoteIOP   iopInfo
	gatewayIOP  iopInfo
	eastwestIOP iopInfo
}

// ID implements resource.Instance
func (i *istioImpl) ID() resource.ID {
	return i.id
}

func (i *istioImpl) Settings() Config {
	return i.cfg
}

func (i *istioImpl) Ingresses() ingress.Instances {
	var out ingress.Instances
	for _, c := range i.ctx.Clusters() {
		// call IngressFor in-case initialization is needed.
		out = append(out, i.IngressFor(c))
	}
	return out
}

func (i *istioImpl) IngressFor(c cluster.Cluster) ingress.Instance {
	ingressServiceName := defaultIngressServiceName
	ingressServiceNamespace := i.cfg.SystemNamespace
	ingressServiceLabel := defaultIngressIstioLabel
	if serviceNameOverride := i.cfg.IngressGatewayServiceName; serviceNameOverride != "" {
		ingressServiceName = serviceNameOverride
	}
	if serviceNamespaceOverride := i.cfg.IngressGatewayServiceNamespace; serviceNamespaceOverride != "" {
		ingressServiceNamespace = serviceNamespaceOverride
	}
	if serviceLabelOverride := i.cfg.IngressGatewayIstioLabel; serviceLabelOverride != "" {
		ingressServiceLabel = fmt.Sprintf("istio=%s", serviceLabelOverride)
	}
	name := types.NamespacedName{Name: ingressServiceName, Namespace: ingressServiceNamespace}
	return i.CustomIngressFor(c, name, ingressServiceLabel)
}

func (i *istioImpl) EastWestGatewayFor(c cluster.Cluster) ingress.Instance {
	name := types.NamespacedName{Name: eastWestIngressServiceName, Namespace: i.cfg.SystemNamespace}
	return i.CustomIngressFor(c, name, eastWestIngressIstioLabel)
}

func (i *istioImpl) CustomIngressFor(c cluster.Cluster, service types.NamespacedName, labelSelector string) ingress.Instance {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.ingress[c.Name()] == nil {
		i.ingress[c.Name()] = map[string]ingress.Instance{}
	}
	if _, ok := i.ingress[c.Name()][labelSelector]; !ok {
		ingr := newIngress(i.ctx, ingressConfig{
			Cluster:       c,
			Service:       service,
			LabelSelector: labelSelector,
		})
		if closer, ok := ingr.(io.Closer); ok {
			i.ctx.Cleanup(func() { _ = closer.Close() })
		}
		i.ingress[c.Name()][labelSelector] = ingr
	}
	return i.ingress[c.Name()][labelSelector]
}

func (i *istioImpl) PodIPsFor(c cluster.Cluster, namespace string, label string) ([]corev1.PodIP, error) {
	// Find the pod with the specified label in the specified namespace
	fetchFn := testKube.NewSinglePodFetch(c, namespace, label)
	pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}

	pod := pods[0]
	return pod.Status.PodIPs, nil
}

func (i *istioImpl) InternalDiscoveryAddressFor(c cluster.Cluster) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if e, f := i.istiod[c.Name()]; f {
		return e.Address(), nil
	}
	// Find the istiod pod and service, and start forwarding a local port.
	fetchFn := testKube.NewSinglePodFetch(c, i.cfg.SystemNamespace, "istio=pilot")
	pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return "", err
	}
	pod := pods[0]
	fw, err := c.NewPortForwarder(pod.Name, pod.Namespace, "", 0, 15012)
	if err != nil {
		return "", err
	}

	if err := fw.Start(); err != nil {
		return "", err
	}
	return fw.Address(), nil
}

func (i *istioImpl) RemoteDiscoveryAddressFor(cluster cluster.Cluster) (netip.AddrPort, error) {
	var addr netip.AddrPort
	primary := cluster.Primary()
	if !primary.IsConfig() {
		// istiod is exposed via LoadBalancer since we won't have ingress outside of a cluster;a cluster that is;
		// a control cluster, but not config cluster is supposed to simulate istiod outside of k8s or "external"
		address, err := retry.UntilComplete(func() (any, bool, error) {
			addrs, outcome, err := getRemoteServiceAddresses(i.env.Settings(), primary, i.cfg.SystemNamespace, istiodLabel, istiodSvcName, discoveryPort)
			return addrs[0], outcome, err
		}, getAddressTimeout, getAddressDelay)
		if err != nil {
			return netip.AddrPort{}, err
		}
		addr = address.(netip.AddrPort)
	} else {
		name := types.NamespacedName{Name: eastWestIngressServiceName, Namespace: i.cfg.SystemNamespace}
		addr = i.CustomIngressFor(primary, name, eastWestIngressIstioLabel).DiscoveryAddresses()[0]
	}
	if !addr.IsValid() {
		return netip.AddrPort{}, fmt.Errorf("failed to get ingress IP for %s", primary.Name())
	}
	return addr, nil
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	cfg.fillDefaults(ctx)

	scopes.Framework.Infof("=== Istio Component Config ===")
	scopes.Framework.Infof("\n%s", cfg.String())
	scopes.Framework.Infof("================================")

	// Top-level work dir for Istio deployment.
	workDir, err := ctx.CreateTmpDirectory("istio-deployment")
	if err != nil {
		return nil, err
	}

	// Generate common IstioOperator yamls for different cluster types (primary, remote, remote-config)
	iopFiles, err := genCommonOperatorFiles(ctx, cfg, workDir)
	if err != nil {
		return nil, err
	}

	// Populate the revisions for the control plane.
	var revisions resource.RevVerMap
	if !cfg.DeployIstio {
		// Using a pre-installed control plane. Get the revisions from the
		// command-line.
		revisions = ctx.Settings().Revisions
	} else if len(iopFiles.primaryIOP.spec.Revision) > 0 {
		// Use revisions from the default control plane operator.
		revisions = resource.RevVerMap{
			iopFiles.primaryIOP.spec.Revision: "",
		}
	}

	i := &istioImpl{
		env:     ctx.Environment().(*kube.Environment),
		cfg:     cfg,
		ctx:     ctx,
		workDir: workDir,
		// TODO
		// values:               iop.Spec.Values.Fields,
		installer:            newInstaller(ctx, workDir),
		meshConfig:           &meshConfig{configMap: *newConfigMap(ctx, cfg.SystemNamespace, revisions)},
		injectConfig:         &injectConfig{configMap: *newConfigMap(ctx, cfg.SystemNamespace, revisions)},
		iopFiles:             iopFiles,
		ingress:              map[string]map[string]ingress.Instance{},
		istiod:               map[string]istiokube.PortForwarder{},
		externalControlPlane: ctx.AllClusters().IsExternalControlPlane(),
	}

	t0 := time.Now()
	defer func() {
		ctx.RecordTraceEvent("istio-deploy", time.Since(t0).Seconds())
	}()
	i.id = ctx.TrackResource(i)

	// Execute External Control Plane Installer Script
	if cfg.ControlPlaneInstaller != "" && !cfg.DeployIstio {
		scopes.Framework.Infof("============= Execute Control Plane Installer =============")
		cmd := exec.Command(cfg.ControlPlaneInstaller, "install", workDir)
		if err := cmd.Run(); err != nil {
			scopes.Framework.Errorf("failed to run external control plane installer: %v", err)
		}
	}

	if !cfg.DeployIstio {
		scopes.Framework.Info("skipping deployment as specified in the config")
		return i, nil
	}

	// For multicluster, create and push the CA certs to all clusters to establish a shared root of trust.
	if i.env.IsMultiCluster() {
		if err := i.deployCACerts(); err != nil {
			return nil, err
		}
	}

	// First install remote-config clusters.
	// We do this first because the external istiod needs to read the config cluster at startup.
	for _, c := range ctx.Clusters().Configs().Remotes() {
		if err = i.installConfigCluster(c); err != nil {
			return i, err
		}
	}

	// Install control plane clusters (can be external or primary).
	errG := multierror.Group{}
	for _, c := range ctx.AllClusters().Primaries() {
		errG.Go(func() error {
			return i.installControlPlaneCluster(c)
		})
	}
	if err := errG.Wait().ErrorOrNil(); err != nil {
		scopes.Framework.Errorf("one or more errors occurred installing control-plane clusters: %v", err)
		return i, err
	}

	// Update config clusters now that external istiod is running.
	for _, c := range ctx.Clusters().Configs().Remotes() {
		if err = i.reinstallConfigCluster(c); err != nil {
			return i, err
		}
	}

	// Install (non-config) remote clusters.
	errG = multierror.Group{}
	for _, c := range ctx.Clusters().Remotes(ctx.Clusters().Configs()...) {
		errG.Go(func() error {
			if err := i.installRemoteCluster(c); err != nil {
				return fmt.Errorf("failed installing remote cluster %s: %v", c.Name(), err)
			}
			return nil
		})
	}
	if errs := errG.Wait(); errs != nil {
		errs.ErrorFormat = istiomultierror.MultiErrorFormat()
		return nil, fmt.Errorf("%d errors occurred deploying remote clusters: %v", errs.Len(), errs.ErrorOrNil())
	}

	if ctx.Clusters().IsMulticluster() && !cfg.SkipDeployCrossClusterSecrets {
		// Need to determine if there is a setting to watch cluster secret in config cluster
		// or in external cluster. The flag is named LOCAL_CLUSTER_SECRET_WATCHER and set as
		// an environment variable for istiod.
		watchLocalNamespace := false
		if i.primaryIOP.spec != nil && i.primaryIOP.spec.Values != nil {
			v, err := values.MapFromJSON(i.primaryIOP.spec.Values)
			if err != nil {
				return nil, err
			}
			localClusterSecretWatcher := v.GetPathString("pilot.env.LOCAL_CLUSTER_SECRET_WATCHER")
			if localClusterSecretWatcher == "true" && i.externalControlPlane {
				watchLocalNamespace = true
			}
		}
		if err := i.configureDirectAPIServerAccess(watchLocalNamespace); err != nil {
			return nil, err
		}
	}

	// Configure gateways for remote clusters.
	for _, c := range ctx.Clusters().Remotes() {
		if i.externalControlPlane || cfg.IstiodlessRemotes {
			// Install ingress and egress gateways
			// These need to be installed as a separate step for external control planes because config clusters are installed
			// before the external control plane cluster. Since remote clusters use gateway injection, we can't install the gateways
			// until after the control plane is running, so we install them here. This is not really necessary for pure (non-config)
			// remote clusters, but it's cleaner to just install gateways as a separate step for all remote clusters.
			if err = i.installRemoteClusterGateways(c); err != nil {
				return i, err
			}
		}

		// remote clusters only need east-west gateway for multi-network purposes
		if ctx.Environment().IsMultiNetwork() {
			spec := i.remoteIOP.spec
			if c.IsConfig() {
				spec = i.configIOP.spec
			}
			if err := i.deployEastWestGateway(c, spec.Revision, i.eastwestIOP.file); err != nil {
				return i, err
			}

			// Wait for the eastwestgateway to have a public IP.
			name := types.NamespacedName{Name: eastWestIngressServiceName, Namespace: i.cfg.SystemNamespace}
			_ = i.CustomIngressFor(c, name, eastWestIngressIstioLabel).DiscoveryAddresses()
		}
	}

	if i.env.IsMultiNetwork() {
		// enable cross network traffic
		for _, c := range ctx.Clusters().Configs() {
			if err := i.exposeUserServices(c); err != nil {
				return nil, err
			}
		}
	}

	return i, nil
}

func initIOPFile(cfg Config, iopFile string, valuesYaml string) (*iopv1alpha1.IstioOperatorSpec, error) {
	operatorYaml := cfg.IstioOperatorConfigYAML(valuesYaml)

	operatorCfg := &iopv1alpha1.IstioOperator{}
	if err := yaml.Unmarshal([]byte(operatorYaml), operatorCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base iop: %v, %v", err, operatorYaml)
	}

	// marshaling entire operatorCfg causes panic because of *time.Time in ObjectMeta
	outb, err := yaml.Marshal(operatorCfg)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling iop spec: %v", err)
	}

	if err := os.WriteFile(iopFile, outb, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write iop: %v", err)
	}

	return &operatorCfg.Spec, nil
}

// installControlPlaneCluster installs the istiod control plane to the given cluster.
// The cluster is considered a "primary" cluster if it is also a "config cluster", in which case components
// like ingress will be installed.
func (i *istioImpl) installControlPlaneCluster(c cluster.Cluster) error {
	scopes.Framework.Infof("setting up %s as control-plane cluster", c.Name())

	if !c.IsConfig() {
		if err := i.configureRemoteConfigForControlPlane(c); err != nil {
			return err
		}
	}

	args := commonInstallArgs(i.ctx, i.cfg, c, i.cfg.PrimaryClusterIOPFile, i.primaryIOP.file)
	if i.ctx.Environment().IsMultiCluster() {
		if i.externalControlPlane || i.cfg.IstiodlessRemotes {
			// Enable namespace controller writing to remote clusters
			args.AppendSet("values.pilot.env.EXTERNAL_ISTIOD", "true")
		}

		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		clusterName := c.Name()
		if !c.IsConfig() {
			clusterName = c.ConfigName()
		}
		args.AppendSet("values.global.multiCluster.clusterName", clusterName)
	}

	if err := i.installer.Install(c, args); err != nil {
		return err
	}

	if c.IsConfig() {
		// this is a traditional primary cluster, install the eastwest gateway

		// there are a few tests that require special gateway setup which will cause eastwest gateway fail to start
		// exclude these tests from installing eastwest gw for now
		if !i.cfg.DeployEastWestGW {
			return nil
		}

		if err := i.deployEastWestGateway(c, i.primaryIOP.spec.Revision, i.eastwestIOP.file); err != nil {
			return err
		}
		// Other clusters should only use this for discovery if its a config cluster.
		if err := i.applyIstiodGateway(c, i.primaryIOP.spec.Revision); err != nil {
			return fmt.Errorf("failed applying istiod gateway for cluster %s: %v", c.Name(), err)
		}
		if err := waitForIstioReady(i.ctx, c, i.cfg); err != nil {
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
func (i *istioImpl) reinstallConfigCluster(c cluster.Cluster) error {
	scopes.Framework.Infof("updating setup for config cluster %s", c.Name())
	return i.installRemoteCommon(c, i.cfg.ConfigClusterIOPFile, i.configIOP.file, true)
}

// installConfigCluster installs istio to a cluster that runs workloads and provides Istio configuration.
// The installed components include CRDs, Roles, etc. but not istiod.
func (i *istioImpl) installConfigCluster(c cluster.Cluster) error {
	scopes.Framework.Infof("setting up %s as config cluster", c.Name())
	return i.installRemoteCommon(c, i.cfg.ConfigClusterIOPFile, i.configIOP.file, false)
}

// installRemoteCluster installs istio to a remote cluster that does not also serve as a config cluster.
func (i *istioImpl) installRemoteCluster(c cluster.Cluster) error {
	scopes.Framework.Infof("setting up %s as remote cluster", c.Name())
	return i.installRemoteCommon(c, i.cfg.RemoteClusterIOPFile, i.remoteIOP.file, true)
}

// Common install on a either a remote-config or pure remote cluster.
func (i *istioImpl) installRemoteCommon(c cluster.Cluster, defaultsIOPFile, iopFile string, discovery bool) error {
	args := commonInstallArgs(i.ctx, i.cfg, c, defaultsIOPFile, iopFile)
	if i.env.IsMultiCluster() {
		// Set the clusterName for the local cluster.
		// This MUST match the clusterName in the remote secret for this cluster.
		args.AppendSet("values.global.multiCluster.clusterName", c.Name())
	}

	if discovery {
		// Configure the cluster and network arguments to pass through the injector webhook.
		remoteIstiodAddress, err := i.RemoteDiscoveryAddressFor(c)
		if err != nil {
			return err
		}
		args.AppendSet("values.global.remotePilotAddress", remoteIstiodAddress.Addr().String())
	}

	if i.externalControlPlane || i.cfg.IstiodlessRemotes {
		args.AppendSet("values.istiodRemote.injectionPath",
			fmt.Sprintf("/inject/net/%s/cluster/%s", c.NetworkName(), c.Name()))
	}

	if err := i.installer.Install(c, args); err != nil {
		return err
	}

	return nil
}

func (i *istioImpl) installRemoteClusterGateways(c cluster.Cluster) error {
	inFilenames := []string{
		filepath.Join(testenv.IstioSrc, IntegrationTestRemoteGatewaysIOP),
	}
	if i.gatewayIOP.file != "" {
		inFilenames = append(inFilenames, i.gatewayIOP.file)
	}
	args := installArgs{
		ComponentName: "ingress and egress gateways",
		Files:         inFilenames,
		Set: []string{
			"values.global.imagePullPolicy=" + i.ctx.Settings().Image.PullPolicy,
		},
	}

	if err := i.installer.Install(c, args); err != nil {
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

func commonInstallArgs(ctx resource.Context, cfg Config, c cluster.Cluster, defaultsIOPFile, iopFile string) installArgs {
	if !path.IsAbs(defaultsIOPFile) {
		defaultsIOPFile = filepath.Join(testenv.IstioSrc, defaultsIOPFile)
	}
	baseIOP := cfg.BaseIOPFile
	if !path.IsAbs(baseIOP) {
		baseIOP = filepath.Join(testenv.IstioSrc, baseIOP)
	}

	args := installArgs{
		Files: []string{
			baseIOP,
			defaultsIOPFile,
			iopFile,
		},
		Set: []string{
			"hub=" + ctx.Settings().Image.Hub,
			"tag=" + ctx.Settings().Image.Tag,
			"values.global.imagePullPolicy=" + ctx.Settings().Image.PullPolicy,
			"values.global.variant=" + ctx.Settings().Image.Variant,
		},
	}

	if ctx.Environment().IsMultiNetwork() && c.NetworkName() != "" {
		args.AppendSet("values.global.meshID", meshID)
		args.AppendSet("values.global.network", c.NetworkName())
	}

	// Include all user-specified values and configuration options.
	if cfg.EnableCNI {
		args.AppendSet("components.cni.enabled", "true")
	}

	// Used in Ambient mode
	if cfg.TrustedZtunnelNamespace != "" {
		args.AppendSet("values.pilot.trustedZtunnelNamespace", cfg.TrustedZtunnelNamespace)
	}

	if len(ctx.Settings().IPFamilies) > 1 {
		args.AppendSet("values.pilot.env.ISTIO_DUAL_STACK", "true")
		args.AppendSet("values.pilot.ipFamilyPolicy", string(corev1.IPFamilyPolicyRequireDualStack))
		args.AppendSet("meshConfig.defaultConfig.proxyMetadata.ISTIO_DUAL_STACK", "true")
		args.AppendSet("values.gateways.istio-ingressgateway.ipFamilyPolicy", string(corev1.IPFamilyPolicyRequireDualStack))
		args.AppendSet("values.gateways.istio-egressgateway.ipFamilyPolicy", string(corev1.IPFamilyPolicyRequireDualStack))
	}

	// Include all user-specified values.
	for k, v := range cfg.Values {
		args.AppendSet("values."+k, v)
	}

	for k, v := range cfg.OperatorOptions {
		args.AppendSet(k, v)
	}
	return args
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

// configureDirectAPIServiceAccessBetweenClusters - create a remote secret of cluster `c` and place
// the secret in all `from` clusters
func (i *istioImpl) configureDirectAPIServiceAccessBetweenClusters(c cluster.Cluster, from ...cluster.Cluster) error {
	// Create a secret.
	secret, err := i.CreateRemoteSecret(i.ctx, c)
	if err != nil {
		return fmt.Errorf("failed creating remote secret for cluster %s: %v", c.Name(), err)
	}
	if err := i.ctx.ConfigKube(from...).
		YAML(i.cfg.SystemNamespace, secret).
		Apply(apply.NoCleanup); err != nil {
		return fmt.Errorf("failed applying remote secret to clusters: %v", err)
	}
	return nil
}

func getTargetClusterListForCluster(targetClusters []cluster.Cluster, c cluster.Cluster) []cluster.Cluster {
	var outClusters []cluster.Cluster
	scopes.Framework.Infof("Secret from cluster: %s will be placed in following clusters", c.Name())
	for _, cc := range targetClusters {
		// if cc is an external cluster, config cluster's secret should have already been
		// placed on the cluster, or the given cluster is the same as the cluster in
		// the target list. Only when c is not config cluster, cc is not external cluster
		// and the given cluster is not the same as the target, c's secret goes onto cc.
		if (!c.IsConfig() || !cc.IsExternalControlPlane()) && c.Name() != cc.Name() {
			scopes.Framework.Infof("Target cluster: %s", cc.Name())
			outClusters = append(outClusters, cc)
		}
	}
	return outClusters
}

func (i *istioImpl) configureDirectAPIServerAccess(watchLocalNamespace bool) error {
	var targetClusters []cluster.Cluster
	if watchLocalNamespace {
		// when configured to watch istiod local namespace, secrets go to the external cluster
		// and primary clusters
		targetClusters = i.ctx.AllClusters().Primaries()
	} else {
		// when configured to watch istiod config namespace, secrets go to config clusters
		targetClusters = i.ctx.AllClusters().Configs()
	}

	// Now look through entire mesh, create secret for every cluster other than external control plane and
	// place the secret into the target clusters.
	for _, c := range i.ctx.Clusters().MeshClusters() {
		theTargetClusters := getTargetClusterListForCluster(targetClusters, c)
		if len(theTargetClusters) > 0 {
			if err := i.configureDirectAPIServiceAccessBetweenClusters(c, theTargetClusters...); err != nil {
				return fmt.Errorf("failed providing primary cluster access for remote cluster %s: %v", c.Name(), err)
			}
		}
	}
	return nil
}

func (i *istioImpl) CreateRemoteSecret(ctx resource.Context, c cluster.Cluster, opts ...string) (string, error) {
	istioCtl, err := istioctl.New(ctx, istioctl.Config{
		Cluster: c,
	})
	if err != nil {
		return "", err
	}
	istioctlCmd := []string{
		"create-remote-secret",
		"--name", c.Name(),
		"--namespace", i.cfg.SystemNamespace,
		"--manifests", filepath.Join(testenv.IstioSrc, "manifests"),
	}
	istioctlCmd = append(istioctlCmd, opts...)

	scopes.Framework.Infof("Creating remote secret for cluster %s %v", c.Name(), istioctlCmd)
	out, _, err := istioCtl.Invoke(istioctlCmd)
	if err != nil {
		return "", fmt.Errorf("create remote secret failed for cluster %s: %v", c.Name(), err)
	}
	return out, nil
}

func (i *istioImpl) deployCACerts() error {
	certsDir := filepath.Join(i.workDir, "cacerts")
	if err := os.Mkdir(certsDir, 0o700); err != nil {
		return err
	}

	root, err := ca.NewRoot(certsDir)
	if err != nil {
		return fmt.Errorf("failed creating the root CA: %v", err)
	}

	for _, c := range i.env.Clusters() {
		// Create a subdir for the cluster certs.
		clusterDir := filepath.Join(certsDir, c.Name())
		if err := os.Mkdir(clusterDir, 0o700); err != nil {
			return err
		}

		// Create the new extensions config for the CA
		caConfig, err := ca.NewIstioConfig(i.cfg.SystemNamespace)
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
		if i.env.IsMultiNetwork() {
			nsLabels = map[string]string{label.TopologyNetwork.Name: c.NetworkName()}
		}
		var nsAnnotations map[string]string
		if c.IsRemote() {
			nsAnnotations = map[string]string{
				annotation.TopologyControlPlaneClusters.Name: c.Config().Name(),
				// ^^^ Use config cluster name because external control plane uses config cluster as its cluster ID
			}
		}
		if _, err := c.Kube().CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      nsLabels,
				Annotations: nsAnnotations,
				Name:        i.cfg.SystemNamespace,
			},
		}, metav1.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := c.Kube().CoreV1().Namespaces().Update(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      nsLabels,
						Annotations: nsAnnotations,
						Name:        i.cfg.SystemNamespace,
					},
				}, metav1.UpdateOptions{}); err != nil {
					scopes.Framework.Errorf("failed updating namespace %s on cluster %s. This can happen when deploying "+
						"multiple control planes. Error: %v", i.cfg.SystemNamespace, c.Name(), err)
				}
			} else {
				scopes.Framework.Errorf("failed creating namespace %s on cluster %s. This can happen when deploying "+
					"multiple control planes. Error: %v", i.cfg.SystemNamespace, c.Name(), err)
			}
		}

		// Create the secret for the cacerts.
		if _, err := c.Kube().CoreV1().Secrets(i.cfg.SystemNamespace).Create(context.TODO(), secret,
			metav1.CreateOptions{}); err != nil {
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
func (i *istioImpl) configureRemoteConfigForControlPlane(c cluster.Cluster) error {
	configCluster := c.Config()
	istioKubeConfig, err := file.AsString(configCluster.(*kubecluster.Cluster).Filename())
	if err != nil {
		scopes.Framework.Infof("error in parsing kubeconfig for %s", configCluster.Name())
		return err
	}

	scopes.Framework.Infof("configuring external control plane in %s to use config cluster %s", c.Name(), configCluster.Name())
	// ensure system namespace exists
	if _, err = c.Kube().CoreV1().Namespaces().
		Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: i.cfg.SystemNamespace,
			},
		}, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	// create kubeconfig secret
	if _, err = c.Kube().CoreV1().Secrets(i.cfg.SystemNamespace).
		Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-kubeconfig",
				Namespace: i.cfg.SystemNamespace,
			},
			Data: map[string][]byte{
				"config": []byte(istioKubeConfig),
			},
		}, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) { // Allow easier running locally when we run multiple tests in a row
			if _, err := c.Kube().CoreV1().Secrets(i.cfg.SystemNamespace).Update(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-kubeconfig",
					Namespace: i.cfg.SystemNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(istioKubeConfig),
				},
			}, metav1.UpdateOptions{}); err != nil {
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

func (i *istioImpl) UpdateInjectionConfig(t resource.Context, update func(*inject.Config) error, cleanup cleanup.Strategy) error {
	return i.injectConfig.UpdateInjectionConfig(t, update, cleanup)
}

func (i *istioImpl) InjectionConfig() (*inject.Config, error) {
	return i.injectConfig.InjectConfig()
}

func genCommonOperatorFiles(ctx resource.Context, cfg Config, workDir string) (i iopFiles, err error) {
	// Generate the istioctl config file for primary clusters
	i.primaryIOP.file = filepath.Join(workDir, "iop.yaml")
	if i.primaryIOP.spec, err = initIOPFile(cfg, i.primaryIOP.file, cfg.ControlPlaneValues); err != nil {
		return iopFiles{}, err
	}

	// Generate the istioctl config file for remote cluster
	i.remoteIOP.file = filepath.Join(workDir, "remote.yaml")
	if i.remoteIOP.spec, err = initIOPFile(cfg, i.remoteIOP.file, cfg.RemoteClusterValues); err != nil {
		return iopFiles{}, err
	}

	// Generate the istioctl config file for config cluster
	if ctx.AllClusters().IsExternalControlPlane() {
		i.configIOP.file = filepath.Join(workDir, "config.yaml")
		if i.configIOP.spec, err = initIOPFile(cfg, i.configIOP.file, cfg.ConfigClusterValues); err != nil {
			return iopFiles{}, err
		}
	} else {
		i.configIOP = i.primaryIOP
	}

	if cfg.GatewayValues != "" {
		i.gatewayIOP.file = filepath.Join(workDir, "custom_gateways.yaml")
		_, err = initIOPFile(cfg, i.gatewayIOP.file, cfg.GatewayValues)
		if err != nil {
			return iopFiles{}, err
		}
	}
	if cfg.EastWestGatewayValues != "" {
		i.eastwestIOP.file = filepath.Join(workDir, "eastwest.yaml")
		_, err = initIOPFile(cfg, i.eastwestIOP.file, cfg.EastWestGatewayValues)
		if err != nil {
			return iopFiles{}, err
		}
	}

	return
}

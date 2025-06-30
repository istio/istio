//go:build integ
// +build integ

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

package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	networking "istio.io/api/networking/v1alpha3"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	IstioNamespace         = "istio-system"
	ReleasePrefix          = "istio-"
	BaseChart              = "base"
	CRDsFolder             = "crds"
	DiscoveryChartsDir     = "istio-discovery"
	BaseReleaseName        = ReleasePrefix + BaseChart
	RepoBaseChartPath      = BaseChart
	RepoDiscoveryChartPath = "istiod"
	RepoGatewayChartPath   = "gateway"
	IstiodReleaseName      = "istiod"
	IngressReleaseName     = "istio-ingress"
	ControlChartsDir       = "istio-control"
	GatewayChartsDir       = "gateway"
	CniChartsDir           = "istio-cni"
	ZtunnelChartsDir       = "ztunnel"
	RepoCniChartPath       = "cni"
	CniReleaseName         = ReleasePrefix + "cni"
	RepoZtunnelChartPath   = "ztunnel"
	ZtunnelReleaseName     = "ztunnel"

	RetryDelay   = 2 * time.Second
	RetryTimeOut = 5 * time.Minute
	Timeout      = 2 * time.Minute

	sampleEnvoyFilter = `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: sample
spec:
  workloadSelector:
    labels:
      istio: ingressgateway
  configPatches:
  - applyTo: NETWORK_FILTER # http connection manager is a filter in Envoy
    match:
      context: GATEWAY
      listener:
        filterChain:
          sni: app.example.com
          filter:
            name: "envoy.filters.network.http_connection_manager"
    patch:
      operation: MERGE
      value:
        typed_config:
          "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
          xff_num_trusted_hops: 5
          common_http_protocol_options:
            idle_timeout: 30s
`

	revisionedSampleEnvoyFilter = `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: sample
  labels:
    istio.io/rev: %s
spec:
  workloadSelector:
    labels:
      istio: ingressgateway
  configPatches:
  - applyTo: NETWORK_FILTER # http connection manager is a filter in Envoy
    match:
      context: GATEWAY
      listener:
        filterChain:
          sni: app.example.com
          filter:
            name: "envoy.filters.network.http_connection_manager"
    patch:
      operation: MERGE
      value:
        typed_config:
          "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
          xff_num_trusted_hops: 5
          common_http_protocol_options:
            idle_timeout: 30s
`

	extendedTelemetry = `
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: sample
spec:
  metrics:
    - providers:
      - name: prometheus
      reportingInterval: 10s
`

	revisionedExtendedTelemetry = `
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: sample
  labels:
    istio.io/rev: %s
spec:
  metrics:
    - providers:
      - name: prometheus
      reportingInterval: 10s
`
)

// ManifestsChartPath is path of local Helm charts used for testing.
var ManifestsChartPath = filepath.Join(env.IstioSrc, "manifests/charts")

// getValuesOverrides returns the values file created to pass into Helm override default values
// for the hub and tag.
//
// Tag can be the empty string, which means the values file will not have a
// tag. In other words, the tag will come from the chart. This is useful in the upgrade
// tests, where we deploy an old Istio version using the chart, and we want to use
// the tag that comes with the chart.
func GetValuesOverrides(ctx framework.TestContext, hub, tag, variant, revision string, isAmbient bool) string {
	workDir := ctx.CreateTmpDirectoryOrFail("helm")

	// Create the default base map string for the values file
	values := map[string]interface{}{
		"global": map[string]interface{}{
			"hub":     hub,
			"variant": variant,
		},
		"revision": revision,
	}

	globalValues := values["global"].(map[string]interface{})
	// Only use a tag value if not empty. Not having a tag in values means: Use the tag directly from the chart
	if tag != "" {
		globalValues["tag"] = tag
	}

	// Handle Openshift platform override if set
	if ctx.Settings().OpenShift {
		globalValues["platform"] = "openshift"
		// TODO: do FLATTEN_GLOBALS_REPLACEMENT to avoid this set
		values["platform"] = "openshift"
	} else {
		globalValues["platform"] = "" // no platform
	}

	// Handle Ambient profile override if set
	if isAmbient {
		values["profile"] = "ambient"
		// Remove revision for ambient profile
		delete(values, "revision")
	}

	// Marshal the map to a YAML string
	overrideValues, err := yaml.Marshal(values)
	if err != nil {
		ctx.Fatalf("failed to marshal override values to YAML: %v", err)
	}

	overrideValuesFile := filepath.Join(workDir, "values.yaml")
	if err := os.WriteFile(overrideValuesFile, overrideValues, os.ModePerm); err != nil {
		ctx.Fatalf("failed to write iop cr file: %v", err)
	}

	return overrideValuesFile
}

var DefaultNamespaceConfig = NewNamespaceConfig()

func NewNamespaceConfig(config ...types.NamespacedName) NamespaceConfig {
	result := make(nsConfig, len(config))
	for _, c := range config {
		result[c.Name] = c.Namespace
	}
	return result
}

type nsConfig map[string]string

func (n nsConfig) Get(name string) string {
	if ns, ok := n[name]; ok {
		return ns
	}
	return IstioNamespace
}

func (n nsConfig) Set(name, ns string) {
	n[name] = ns
}

func (n nsConfig) AllNamespaces() []string {
	return maps.Values(n)
}

type NamespaceConfig interface {
	Get(name string) string
	Set(name, ns string)
	AllNamespaces() []string
}

// InstallIstio install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func InstallIstio(t framework.TestContext, cs cluster.Cluster, h *helm.Helm, overrideValuesFile,
	version string, installGateway bool, ambientProfile bool, nsConfig NamespaceConfig,
) {
	for _, ns := range nsConfig.AllNamespaces() {
		CreateNamespace(t, cs, ns)
	}
	CreateNamespace(t, cs, IstioNamespace)

	versionArgs := ""

	baseChartPath := RepoBaseChartPath
	discoveryChartPath := RepoDiscoveryChartPath
	gatewayChartPath := RepoGatewayChartPath
	cniChartPath := RepoCniChartPath
	ztunnelChartPath := RepoZtunnelChartPath

	gatewayOverrideValuesFile := overrideValuesFile

	if version != "" {
		// Prepend ~ to the version, so that we can refer to the latest patch version of a minor version
		versionArgs = fmt.Sprintf("--repo %s --version ~%s", t.Settings().HelmRepo, version)
	} else {
		baseChartPath = filepath.Join(ManifestsChartPath, BaseChart)
		discoveryChartPath = filepath.Join(ManifestsChartPath, ControlChartsDir, DiscoveryChartsDir)
		gatewayChartPath = filepath.Join(ManifestsChartPath, version, GatewayChartsDir)
		cniChartPath = filepath.Join(ManifestsChartPath, version, CniChartsDir)
		ztunnelChartPath = filepath.Join(ManifestsChartPath, version, ZtunnelChartsDir)

	}

	// Install base chart
	err := h.InstallChart(BaseReleaseName, baseChartPath, nsConfig.Get(BaseReleaseName), overrideValuesFile, Timeout, versionArgs)
	if err != nil {
		t.Fatalf("failed to install istio %s chart: %v", BaseChart, err)
	}

	// Install discovery chart
	err = h.InstallChart(IstiodReleaseName, discoveryChartPath, nsConfig.Get(IstiodReleaseName), overrideValuesFile, Timeout, versionArgs)
	if err != nil {
		t.Fatalf("failed to install istio %s chart: %v", DiscoveryChartsDir, err)
	}

	if installGateway {
		err = h.InstallChart(IngressReleaseName, gatewayChartPath, nsConfig.Get(IngressReleaseName), gatewayOverrideValuesFile, Timeout, versionArgs)
		if err != nil {
			t.Fatalf("failed to install istio %s chart: %v", GatewayChartsDir, err)
		}
	}

	if ambientProfile || t.Settings().OpenShift {
		// Install cni chart
		err = h.InstallChart(CniReleaseName, cniChartPath, nsConfig.Get(CniReleaseName), overrideValuesFile, Timeout, versionArgs)
		if err != nil {
			t.Fatalf("failed to install istio %s chart: %v", CniChartsDir, err)
		}
	}

	if ambientProfile {

		// Install ztunnel chart
		err = h.InstallChart(ZtunnelReleaseName, ztunnelChartPath, nsConfig.Get(ZtunnelReleaseName), overrideValuesFile, Timeout, versionArgs)
		if err != nil {
			t.Fatalf("failed to install istio %s chart: %v", ZtunnelChartsDir, err)
		}
	}
}

// InstallIstioWithRevision install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func InstallIstioWithRevision(t framework.TestContext, cs cluster.Cluster,
	h *helm.Helm, version, revision, overrideValuesFile string, upgradeBaseChart, useTestData bool,
) {
	CreateNamespace(t, cs, IstioNamespace)
	versionArgs := ""
	baseChartPath := RepoBaseChartPath
	discoveryChartPath := RepoDiscoveryChartPath
	if version != "" {
		// Prepend ~ to the version, so that we can refer to the latest patch version of a minor version
		versionArgs = fmt.Sprintf("--repo %s --version ~%s", t.Settings().HelmRepo, version)
	} else {
		baseChartPath = filepath.Join(ManifestsChartPath, BaseChart)
		discoveryChartPath = filepath.Join(ManifestsChartPath, ControlChartsDir, DiscoveryChartsDir)
	}
	// base chart may already be installed if the Istio was previously already installed
	if upgradeBaseChart {
		// upgrade is always done with ../manifests/charts/base
		err := h.UpgradeChart(BaseReleaseName, filepath.Join(ManifestsChartPath, BaseChart),
			IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to upgrade istio %s chart", BaseChart)
		}
	} else {
		err := h.InstallChart(BaseReleaseName, baseChartPath, IstioNamespace, overrideValuesFile, Timeout, versionArgs)
		if err != nil {
			t.Fatalf("failed to upgrade istio %s chart", BaseChart)
		}
	}

	// install discovery chart with --set revision=NAME
	if useTestData {
		err := h.InstallChart(IstiodReleaseName+"-"+revision, discoveryChartPath, IstioNamespace, overrideValuesFile, Timeout, versionArgs)
		if err != nil {
			t.Fatalf("failed to install istio %s chart", DiscoveryChartsDir)
		}
	} else {
		err := h.InstallChart(IstiodReleaseName+"-"+revision, filepath.Join(ManifestsChartPath, ControlChartsDir, DiscoveryChartsDir),
			IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to install istio %s chart", DiscoveryChartsDir)
		}

	}
}

func CreateNamespace(t test.Failer, cs cluster.Cluster, namespace string) {
	if _, err := cs.Kube().CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			log.Debugf("%v namespace already exist", IstioNamespace)
		} else {
			t.Fatalf("failed to create %v namespace: %v", IstioNamespace, err)
		}
	}
}

// DeleteIstio deletes installed Istio Helm charts and resources
func DeleteIstio(t framework.TestContext, h *helm.Helm, cs *kube.Cluster, config NamespaceConfig, isAmbient bool) {
	scopes.Framework.Infof("cleaning up resources")
	if err := h.DeleteChart(IngressReleaseName, config.Get(IngressReleaseName)); err != nil {
		t.Errorf("failed to delete %s release: %v", IngressReleaseName, err)
	}
	if err := h.DeleteChart(IstiodReleaseName, config.Get(IstiodReleaseName)); err != nil {
		t.Errorf("failed to delete %s release: %v", IstiodReleaseName, err)
	}
	if isAmbient {
		if err := h.DeleteChart(ZtunnelReleaseName, config.Get(ZtunnelReleaseName)); err != nil {
			t.Errorf("failed to delete %s release: %v", ZtunnelReleaseName, err)
		}
	}
	if isAmbient || t.Settings().OpenShift {
		if err := h.DeleteChart(CniReleaseName, config.Get(CniReleaseName)); err != nil {
			t.Errorf("failed to delete %s release: %v", CniReleaseName, err)
		}
	}
	if err := h.DeleteChart(BaseReleaseName, config.Get(BaseReleaseName)); err != nil {
		t.Errorf("failed to delete %s release: %v", BaseReleaseName, err)
	}
	g := errgroup.Group{}
	for _, ns := range config.AllNamespaces() {
		if ns == constants.KubeSystemNamespace {
			continue
		}
		g.Go(func() error {
			if err := cs.Kube().CoreV1().Namespaces().Delete(context.TODO(), ns, metav1.DeleteOptions{}); err != nil {
				return fmt.Errorf("failed to delete %s namespace: %v", ns, err)
			}
			if err := kubetest.WaitForNamespaceDeletion(cs.Kube(), ns, retry.Timeout(RetryTimeOut)); err != nil {
				return fmt.Errorf("waiting for %s namespace to be deleted: %v", ns, err)
			}
			return nil
		})
	}
	// try to delete all leader election locks. Istiod will drop them on shutdown, but `helm delete` ordering removes the
	// Role allowing it to do so before it is able to, so it ends up failing to do so.
	// Help it out to ensure the next test doesn't need to wait 30s.
	g.Go(func() error {
		locks := []string{
			leaderelection.NamespaceController,
			leaderelection.GatewayDeploymentController,
			leaderelection.GatewayStatusController,
			leaderelection.IngressController,
		}
		for _, lock := range locks {
			_ = cs.Kube().CoreV1().ConfigMaps(config.Get(IstiodReleaseName)).Delete(context.Background(), lock, metav1.DeleteOptions{})
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
}

// VerifyInstallation verify that the Helm installation is successful
func VerifyPodReady(ctx framework.TestContext, cs cluster.Cluster, ns, label string) {
	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, ns, label)); err != nil {
			return fmt.Errorf("%s pod is not ready: %v", label, err)
		}
		return nil
	}, retry.Timeout(RetryTimeOut), retry.Delay(RetryDelay))
}

// VerifyInstallation verify that the Helm installation is successful
func VerifyInstallation(ctx framework.TestContext, cs cluster.Cluster, nsConfig NamespaceConfig, verifyGateway bool, verifyAmbient bool, revision string) {
	scopes.Framework.Infof("=== verifying istio installation === ")

	validatingwebhookName := "istiod-default-validator"
	if revision != "" {
		validatingwebhookName = fmt.Sprintf("istio-validator-%s-istio-system", revision)
	}
	VerifyValidatingWebhookConfigurations(ctx, cs, []string{
		validatingwebhookName,
	})

	VerifyPodReady(ctx, cs, nsConfig.Get(IstiodReleaseName), "app=istiod")
	if verifyAmbient || ctx.Settings().OpenShift {
		VerifyPodReady(ctx, cs, nsConfig.Get(CniReleaseName), "k8s-app=istio-cni-node")
	}
	if verifyAmbient {
		VerifyPodReady(ctx, cs, nsConfig.Get(ZtunnelReleaseName), "app=ztunnel")
	}
	if verifyGateway {
		VerifyPodReady(ctx, cs, nsConfig.Get(IngressReleaseName), "app=istio-ingress")
	}
	scopes.Framework.Infof("=== succeeded ===")
}

func SetRevisionTagWithVersion(ctx framework.TestContext, h *helm.Helm, revision, revisionTag, version string) {
	scopes.Framework.Infof("=== setting revision tag with version === ")
	// Prepend ~ to the version, so that we can refer to the latest patch version of a minor version
	template, err := h.Template(IstiodReleaseName+"-"+revision, RepoDiscoveryChartPath,
		IstioNamespace, "templates/revision-tags.yaml", Timeout, "--version", "~"+version, "--repo", ctx.Settings().HelmRepo, "--set",
		fmt.Sprintf("revision=%s", revision), "--set", fmt.Sprintf("revisionTags={%s}", revisionTag))
	if err != nil {
		ctx.Fatalf("failed to install istio %s chart", DiscoveryChartsDir)
	}

	err = ctx.ConfigIstio().YAML(IstioNamespace, template).Apply()
	if err != nil {
		ctx.Fatalf("failed to apply templated revision tags yaml: %v", err)
	}

	scopes.Framework.Infof("=== succeeded === ")
}

func SetRevisionTag(ctx framework.TestContext, h *helm.Helm, fileSuffix, revision, revisionTag, relPath, version string) {
	scopes.Framework.Infof("=== setting revision tag === ")
	template, err := h.Template(IstiodReleaseName+"-"+revision, filepath.Join(relPath, version, ControlChartsDir, DiscoveryChartsDir)+fileSuffix,
		IstioNamespace, "templates/revision-tags.yaml", Timeout, "--set",
		fmt.Sprintf("revision=%s", revision), "--set", fmt.Sprintf("revisionTags={%s}", revisionTag))
	if err != nil {
		ctx.Fatalf("failed to install istio %s chart", DiscoveryChartsDir)
	}

	err = ctx.ConfigIstio().YAML(IstioNamespace, template).Apply()
	if err != nil {
		ctx.Fatalf("failed to apply templated revision tags yaml: %v", err)
	}

	scopes.Framework.Infof("=== succeeded === ")
}

// VerifyMutatingWebhookConfigurations verifies that the proper number of mutating webhooks are running, used with
// revisions and revision tags
func VerifyMutatingWebhookConfigurations(ctx framework.TestContext, cs cluster.Cluster, names []string) {
	scopes.Framework.Infof("=== verifying mutating webhook configurations === ")
	if ok := kubetest.MutatingWebhookConfigurationsExists(cs.Kube(), names); !ok {
		ctx.Fatalf("Not all mutating webhook configurations were installed. Expected [%v]", names)
	}
	scopes.Framework.Infof("=== succeeded ===")
}

// VerifyValidatingWebhookConfigurations verifies that the proper number of validating webhooks are running, used with
// revisions and revision tags
func VerifyValidatingWebhookConfigurations(ctx framework.TestContext, cs cluster.Cluster, names []string) {
	scopes.Framework.Infof("=== verifying validating webhook configurations === ")
	if ok := kubetest.ValidatingWebhookConfigurationsExists(cs.Kube(), names); !ok {
		ctx.Fatalf("Not all validating webhook configurations were installed. Expected [%v]", names)
	}
	scopes.Framework.Infof("=== succeeded ===")
}

// verifyValidation verifies that Istio resource validation is active on the cluster.
func verifyValidation(ctx framework.TestContext, revision string) {
	ctx.Helper()
	invalidGateway := &clientnetworking.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-istio-gateway",
			Namespace: IstioNamespace,
		},
		Spec: networking.Gateway{},
	}

	if revision != "" {
		invalidGateway.Labels = map[string]string{
			"istio.io/rev": revision,
		}
	}
	createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	istioClient := ctx.Clusters().Default().Istio().NetworkingV1()
	retry.UntilOrFail(ctx, func() bool {
		_, err := istioClient.Gateways(IstioNamespace).Create(context.TODO(), invalidGateway, createOptions)
		rejected := err != nil
		return rejected
	})
}

// TODO BML this relabeling/reannotating is only required if the previous release is =< 1.23,
// and should be dropped once 1.24 is released.
func AdoptPre123CRDResourcesIfNeeded() {
	requiredAdoptionLabels := []string{"app.kubernetes.io/managed-by=Helm"}
	requiredAdoptionAnnos := []string{"meta.helm.sh/release-name=istio-base", "meta.helm.sh/release-namespace=istio-system"}

	for _, labelToAdd := range requiredAdoptionLabels {
		// We have wildly inconsistent existing labeling pre-1.24, so have to cover all possible cases.
		execCmd1 := fmt.Sprintf("kubectl label crds -l chart=istio %v", labelToAdd)
		execCmd2 := fmt.Sprintf("kubectl label crds -l app.kubernetes.io/part-of=istio %v", labelToAdd)
		_, err1 := shell.Execute(false, execCmd1)
		_, err2 := shell.Execute(false, execCmd2)
		if errors.Join(err1, err2) != nil {
			scopes.Framework.Infof("couldn't relabel CRDs for Helm adoption: %s. Likely not needed for this release", labelToAdd)
		}
	}

	for _, annoToAdd := range requiredAdoptionAnnos {
		// We have wildly inconsistent existing labeling pre-1.24, so have to cover all possible cases.
		execCmd1 := fmt.Sprintf("kubectl annotate crds -l chart=istio %v", annoToAdd)
		execCmd2 := fmt.Sprintf("kubectl annotate crds -l app.kubernetes.io/part-of=istio %v", annoToAdd)
		_, err1 := shell.Execute(false, execCmd1)
		_, err2 := shell.Execute(false, execCmd2)
		if errors.Join(err1, err2) != nil {
			scopes.Framework.Infof("couldn't reannotate CRDs for Helm adoption: %s. Likely not needed for this release", annoToAdd)
		}
	}
}

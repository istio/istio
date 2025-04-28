//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package istio

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

const (
	// DefaultSystemNamespace default value for SystemNamespace
	DefaultSystemNamespace = "istio-system"

	// IntegrationTestDefaultsIOP is the path of the default IstioOperator spec to use
	// for integration tests
	IntegrationTestDefaultsIOP = "tests/integration/iop-integration-test-defaults.yaml"

	// IntegrationTestDefaultsIOPWithQUIC is the path of the default IstioOperator spec to
	// use for integration tests involving QUIC
	IntegrationTestDefaultsIOPWithQUIC = "tests/integration/iop-integration-test-defaults-with-quic.yaml"

	// IntegrationTestRemoteDefaultsIOP is the path of the default IstioOperator spec to use
	// on remote clusters for integration tests
	IntegrationTestRemoteDefaultsIOP = "tests/integration/iop-remote-integration-test-defaults.yaml"

	// BaseIOP is the path of the base IstioOperator spec
	BaseIOP = "tests/integration/base.yaml"

	// IntegrationTestRemoteGatewaysIOP is the path of the default IstioOperator spec to use
	// to install gateways on remote clusters for integration tests
	IntegrationTestRemoteGatewaysIOP = "tests/integration/iop-remote-integration-test-gateways.yaml"

	// IntegrationTestExternalIstiodPrimaryDefaultsIOP is the path of the default IstioOperator spec to use
	// on external istiod primary clusters for integration tests
	IntegrationTestExternalIstiodPrimaryDefaultsIOP = "tests/integration/iop-externalistiod-primary-integration-test-defaults.yaml"

	// IntegrationTestExternalIstiodConfigDefaultsIOP is the path of the default IstioOperator spec to use
	// on external istiod config clusters for integration tests
	IntegrationTestExternalIstiodConfigDefaultsIOP = "tests/integration/iop-externalistiod-config-integration-test-defaults.yaml"

	// IntegrationTestAmbientDefaultsIOP is the path of the default IstioOperator for ambient
	IntegrationTestAmbientDefaultsIOP = "tests/integration/iop-ambient-test-defaults.yaml"

	// IntegrationTestPeerMetadataDiscoveryDefaultsIOP is the path of the default IstioOperator to force WDS usage
	IntegrationTestPeerMetadataDiscoveryDefaultsIOP = "tests/integration/iop-wds.yaml"

	// hubValuesKey values key for the Docker image hub.
	hubValuesKey = "values.global.hub"

	// tagValuesKey values key for the Docker image tag.
	tagValuesKey = "values.global.tag"

	// variantValuesKey values key for the Docker image variant.
	variantValuesKey = "values.global.variant"

	// imagePullPolicyValuesKey values key for the Docker image pull policy.
	imagePullPolicyValuesKey = "values.global.imagePullPolicy"

	// DefaultEgressGatewayLabel is the default Istio label for the egress gateway.
	DefaultEgressGatewayIstioLabel = "egressgateway"

	// DefaultEgressGatewayServiceName is the default service name for the egress gateway.
	DefaultEgressGatewayServiceName = "istio-egressgateway"
)

var (
	helmValues      string
	operatorOptions string

	settingsFromCommandline = &Config{
		SystemNamespace:               DefaultSystemNamespace,
		TelemetryNamespace:            DefaultSystemNamespace,
		DeployIstio:                   true,
		PrimaryClusterIOPFile:         IntegrationTestDefaultsIOP,
		ConfigClusterIOPFile:          IntegrationTestDefaultsIOP,
		RemoteClusterIOPFile:          IntegrationTestRemoteDefaultsIOP,
		BaseIOPFile:                   BaseIOP,
		DeployEastWestGW:              true,
		DumpKubernetesManifests:       false,
		IstiodlessRemotes:             true,
		EnableCNI:                     false,
		EgressGatewayServiceNamespace: DefaultSystemNamespace,
		EgressGatewayServiceName:      DefaultEgressGatewayServiceName,
		EgressGatewayIstioLabel:       DefaultEgressGatewayIstioLabel,
	}
)

// Config provide kube-specific Config from flags.
type Config struct {
	// The namespace where the Istio components (<=1.1) reside in a typical deployment (default: "istio-system").
	SystemNamespace string

	// The namespace in which kiali, tracing providers, graphana, prometheus are deployed.
	TelemetryNamespace string

	// The IstioOperator spec file to be used for Control plane cluster by default
	PrimaryClusterIOPFile string

	// The IstioOperator spec file to be used for Config cluster by default
	ConfigClusterIOPFile string

	// The IstioOperator spec file to be used for Remote cluster by default
	RemoteClusterIOPFile string

	// The IstioOperator spec file used as the base for all installs
	BaseIOPFile string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are applied to non-remote clusters
	ControlPlaneValues string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are only applied to remote clusters
	// Default value will be ControlPlaneValues if no remote values provided
	RemoteClusterValues string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// These values are only applied to remote config clusters
	// Default value will be ControlPlaneValues if no remote values provided
	ConfigClusterValues string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// Do not wait for the validation webhook before completing the deployment. This is useful for
	// doing deployments without Galley.
	SkipWaitForValidationWebhook bool

	// Indicates that the test should deploy Istio's east west gateway into the target Kubernetes cluster
	// before running tests.
	DeployEastWestGW bool

	// SkipDeployCrossClusterSecrets, if enabled, will skip creation of multi-cluster secrets for cross-cluster discovery.
	SkipDeployCrossClusterSecrets bool

	// DumpKubernetesManifests will cause Kubernetes YAML generated by istioctl install/generate to be dumped to artifacts.
	DumpKubernetesManifests bool

	// IstiodlessRemotes makes remote clusters run without istiod, using webhooks/ca from the primary cluster.
	// TODO we could set this per-cluster if istiod was smarter about patching remotes.
	IstiodlessRemotes bool

	// OperatorOptions overrides default operator configuration.
	OperatorOptions map[string]string

	// EnableCNI indicates the test should have CNI enabled.
	EnableCNI bool

	// custom deployment for ingress and egress gateway on remote clusters.
	GatewayValues string

	// Custom deployment for east-west gateway
	EastWestGatewayValues string

	// IngressGatewayServiceName is the service name to use to reference the ingressgateway
	// This field should only be set when DeployIstio is false
	IngressGatewayServiceName string

	// IngressGatewayServiceNamespace allows overriding the namespace of the ingressgateway service (defaults to SystemNamespace)
	// This field should only be set when DeployIstio is false
	IngressGatewayServiceNamespace string

	// IngressGatewayIstioLabel allows overriding the selector of the ingressgateway service (defaults to istio=ingressgateway)
	// This field should only be set when DeployIstio is false
	IngressGatewayIstioLabel string

	// EgressGatewayServiceName is the service name to use to reference the egressgateway
	// This field should only be set when DeployIstio is false
	EgressGatewayServiceName string

	// EgressGatewayServiceNamespace allows overriding the namespace of the egressgateway service (defaults to SystemNamespace)
	// This field should only be set when DeployIstio is false
	EgressGatewayServiceNamespace string

	// EgressGatewayIstioLabel allows overriding the selector of the egressgateway service (defaults to istio=egressgateway)
	// This field should only be set when DeployIstio is false
	EgressGatewayIstioLabel string

	// SharedMeshConfigName is the name of the user's local ConfigMap to be patched, which the user sets as the SHARED_MESH_CONFIG pilot env variable
	// upon installing Istio.
	// This field should only be set when DeployIstio is false.
	SharedMeshConfigName string

	// ControlPlaneInstaller allows installation of custom control planes on istio deployments via an external script
	// This field should only be set when DeployIstio is false
	ControlPlaneInstaller string
}

func (c *Config) OverridesYAML(s *resource.Settings) string {
	return fmt.Sprintf(`
global:
  hub: %s
  tag: %s
`, s.Image.Hub, s.Image.Tag)
}

func (c *Config) IstioOperatorConfigYAML(iopYaml string) string {
	data := ""
	if iopYaml != "" {
		data = Indent(iopYaml, "  ")
	}

	return fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
%s
`, data)
}

func (c *Config) fillDefaults(ctx resource.Context) {
	if ctx.AllClusters().IsExternalControlPlane() {
		c.PrimaryClusterIOPFile = IntegrationTestExternalIstiodPrimaryDefaultsIOP
		c.ConfigClusterIOPFile = IntegrationTestExternalIstiodConfigDefaultsIOP
		if c.ConfigClusterValues == "" {
			c.ConfigClusterValues = c.RemoteClusterValues
		}
	} else if !c.IstiodlessRemotes {
		c.RemoteClusterIOPFile = IntegrationTestDefaultsIOP
		if c.RemoteClusterValues == "" {
			c.RemoteClusterValues = c.ControlPlaneValues
		}
	}
}

// Indent indents a block of text with an indent string
func Indent(text, indent string) string {
	if text[len(text)-1:] == "\n" {
		result := ""
		for _, j := range strings.Split(text[:len(text)-1], "\n") {
			result += indent + j + "\n"
		}
		return result
	}
	result := ""
	for _, j := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		result += indent + j + "\n"
	}
	return result[:len(result)-1]
}

// DefaultConfig creates a new Config from defaults, environments variables, and command-line parameters.
func DefaultConfig(ctx resource.Context) (Config, error) {
	// Make a local copy.
	s := *settingsFromCommandline

	iopFile := s.PrimaryClusterIOPFile
	if iopFile != "" && !path.IsAbs(s.PrimaryClusterIOPFile) {
		iopFile = filepath.Join(env.IstioSrc, s.PrimaryClusterIOPFile)
	}

	if err := checkFileExists(iopFile); err != nil {
		scopes.Framework.Warnf("Default IOPFile missing: %v", err)
	}

	var err error
	if s.OperatorOptions, err = defaultOperatorOptions(ctx); err != nil {
		return Config{}, err
	}

	if operatorOptions, err := parseConfigOptions(operatorOptions, ""); err != nil {
		return Config{}, err
	} else if len(operatorOptions) > 0 {
		maps.MergeCopy(s.OperatorOptions, operatorOptions)
	}

	return s, nil
}

// DefaultConfigOrFail calls DefaultConfig and fails t if an error occurs.
func DefaultConfigOrFail(t test.Failer, ctx resource.Context) Config {
	cfg, err := DefaultConfig(ctx)
	if err != nil {
		t.Fatalf("Get istio config: %v", err)
	}
	return cfg
}

func checkFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
}

func defaultOperatorOptions(ctx resource.Context) (map[string]string, error) {
	userValues, err := parseConfigOptions(helmValues, "values.")
	if err != nil {
		return nil, err
	}

	// Copy the defaults first.
	values := make(map[string]string)

	// Common values
	s := ctx.Settings()
	values[hubValuesKey] = s.Image.Hub
	values[tagValuesKey] = s.Image.Tag
	values[variantValuesKey] = s.Image.Variant
	values[imagePullPolicyValuesKey] = s.Image.PullPolicy

	// Copy the user values.
	for k, v := range userValues {
		values[k] = v
	}

	// Always pull Docker images if using the "latest".
	if values[tagValuesKey] == "latest" {
		values[imagePullPolicyValuesKey] = string(corev1.PullAlways)
	}

	// We need more information on Envoy logs to detect usage of any deprecated feature
	if ctx.Settings().FailOnDeprecation {
		values["values.global.proxy.logLevel"] = "debug"
		values["values.global.proxy.componentLogLevel"] = "misc:debug"
	}

	return values, nil
}

func parseConfigOptions(options string, prefix string) (map[string]string, error) {
	out := make(map[string]string)
	if options == "" {
		return out, nil
	}

	values := strings.Split(options, ",")
	for _, v := range values {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing config options: %s", options)
		}
		out[prefix+strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
}

// String implements fmt.Stringer
func (c *Config) String() string {
	result := ""

	result += fmt.Sprintf("SystemNamespace:                %s\n", c.SystemNamespace)
	result += fmt.Sprintf("TelemetryNamespace:             %s\n", c.TelemetryNamespace)
	result += fmt.Sprintf("DeployIstio:                    %v\n", c.DeployIstio)
	result += fmt.Sprintf("DeployEastWestGW:               %v\n", c.DeployEastWestGW)
	result += fmt.Sprintf("PrimaryClusterIOPFile:          %s\n", c.PrimaryClusterIOPFile)
	result += fmt.Sprintf("ConfigClusterIOPFile:           %s\n", c.ConfigClusterIOPFile)
	result += fmt.Sprintf("RemoteClusterIOPFile:           %s\n", c.RemoteClusterIOPFile)
	result += fmt.Sprintf("BaseIOPFile:                    %s\n", c.BaseIOPFile)
	result += fmt.Sprintf("SkipWaitForValidationWebhook:   %v\n", c.SkipWaitForValidationWebhook)
	result += fmt.Sprintf("DumpKubernetesManifests:        %v\n", c.DumpKubernetesManifests)
	result += fmt.Sprintf("IstiodlessRemotes:              %v\n", c.IstiodlessRemotes)
	result += fmt.Sprintf("OperatorOptions:                %v\n", c.OperatorOptions)
	result += fmt.Sprintf("EnableCNI:                      %v\n", c.EnableCNI)
	result += fmt.Sprintf("IngressGatewayServiceName:      %v\n", c.IngressGatewayServiceName)
	result += fmt.Sprintf("IngressGatewayServiceNamespace: %v\n", c.IngressGatewayServiceNamespace)
	result += fmt.Sprintf("IngressGatewayIstioLabel:       %v\n", c.IngressGatewayIstioLabel)
	result += fmt.Sprintf("EgressGatewayServiceName:       %v\n", c.EgressGatewayServiceName)
	result += fmt.Sprintf("EgressGatewayServiceNamespace:  %v\n", c.EgressGatewayServiceNamespace)
	result += fmt.Sprintf("EgressGatewayIstioLabel:        %v\n", c.EgressGatewayIstioLabel)
	result += fmt.Sprintf("SharedMeshConfigName:           %v\n", c.SharedMeshConfigName)
	result += fmt.Sprintf("ControlPlaneInstaller:          %v\n", c.ControlPlaneInstaller)

	return result
}

// ClaimSystemNamespace retrieves the namespace for the Istio system components from the environment.
func ClaimSystemNamespace(ctx resource.Context) (namespace.Instance, error) {
	istioCfg, err := DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	nsCfg := namespace.Config{
		Prefix: istioCfg.SystemNamespace,
		Inject: false,
		// Already handled directly
		SkipDump:    true,
		SkipCleanup: true,
	}
	return namespace.Claim(ctx, nsCfg)
}

// ClaimSystemNamespaceOrFail calls ClaimSystemNamespace, failing the test if an error occurs.
func ClaimSystemNamespaceOrFail(t test.Failer, ctx resource.Context) namespace.Instance {
	t.Helper()
	i, err := ClaimSystemNamespace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

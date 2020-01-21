//  Copyright 2018 Istio Authors
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
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"

	yaml2 "gopkg.in/yaml.v2"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/core/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"

	kubeCore "k8s.io/api/core/v1"
)

const (
	// DefaultSystemNamespace default value for SystemNamespace
	DefaultSystemNamespace = "istio-system"

	// E2EValuesFile for default settings for Istio Helm deployment.
	// This modifies a few values to help tests, like prometheus scrape interval
	// In general, specific settings should be added to tests, not here
	E2EValuesFile = "test-values/values-integ.yaml"

	// DefaultDeployTimeout for Istio
	DefaultDeployTimeout = time.Second * 300

	// DefaultCIDeployTimeout for Istio
	DefaultCIDeployTimeout = time.Minute * 10

	// DefaultUndeployTimeout for Istio.
	DefaultUndeployTimeout = time.Second * 300

	// DefaultCIUndeployTimeout for Istio.
	DefaultCIUndeployTimeout = time.Second * 900
)

var (
	helmValues string

	settingsFromCommandline = &Config{
		SystemNamespace:                DefaultSystemNamespace,
		IstioNamespace:                 DefaultSystemNamespace,
		ConfigNamespace:                DefaultSystemNamespace,
		TelemetryNamespace:             DefaultSystemNamespace,
		PolicyNamespace:                DefaultSystemNamespace,
		IngressNamespace:               DefaultSystemNamespace,
		EgressNamespace:                DefaultSystemNamespace,
		Operator:                       false,
		DeployIstio:                    true,
		DeployTimeout:                  0,
		UndeployTimeout:                0,
		ChartDir:                       env.IstioChartDir,
		CrdsFilesDir:                   env.CrdsFilesDir,
		ValuesFile:                     E2EValuesFile,
		CustomSidecarInjectorNamespace: "",
	}
)

// Config provide kube-specific Config from flags.
type Config struct {
	// The namespace where the Istio components (<=1.1) reside in a typical deployment (default: "istio-system").
	SystemNamespace string

	// The namespace in which istio ca and cert provisioning components are deployed.
	IstioNamespace string

	// The namespace in which config, discovery and auto-injector are deployed.
	ConfigNamespace string

	// The namespace in which mixer, kiali, tracing providers, graphana, prometheus are deployed.
	TelemetryNamespace string

	// The namespace in which istio policy checker is deployed.
	PolicyNamespace string

	// The namespace in which istio ingressgateway is deployed
	IngressNamespace string

	// The namespace in which istio egressgateway is deployed
	EgressNamespace string

	// DeployTimeout the timeout for deploying Istio.
	DeployTimeout time.Duration

	// UndeployTimeout the timeout for undeploying Istio.
	UndeployTimeout time.Duration

	// The top-level Helm chart dir.
	ChartDir string

	// The top-level Helm Crds files dir.
	CrdsFilesDir string

	// The Helm values file to be used.
	ValuesFile string

	// Override values specifically for the ICP crd
	// This is mostly required for cases where --set cannot be used
	// If specified, Values will be ignored
	ControlPlaneValues string

	// Overrides for the Helm values file.
	Values map[string]string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// Operator determines if we should use the operator for installation
	Operator bool

	// Do not wait for the validation webhook before completing the deployment. This is useful for
	// doing deployments without Galley.
	SkipWaitForValidationWebhook bool

	// CustomSidecarInjectorNamespace allows injecting the sidecar from the specified namespace.
	// if the value is "", use the default sidecar injection instead.
	CustomSidecarInjectorNamespace string
}

func (c *Config) IsIstiodEnabled() bool {
	if c.Operator {
		return c.Values["global.istiod.enabled"] != "false"
	}
	return false
}

// IsMtlsEnabled checks in Values flag and Values file.
func (c *Config) IsMtlsEnabled() bool {
	if c.Values["global.mtls.enabled"] == "true" ||
		c.Values["global.mtls.auto"] == "true" {
		return true
	}

	data, err := file.AsString(filepath.Join(c.ChartDir, c.ValuesFile))
	if err != nil {
		return true
	}
	m := make(map[interface{}]interface{})
	err = yaml2.Unmarshal([]byte(data), &m)
	if err != nil {
		return false
	}
	if m["global"] != nil {
		switch globalVal := m["global"].(type) {
		case map[interface{}]interface{}:
			switch mtlsVal := globalVal["mtls"].(type) {
			case map[interface{}]interface{}:
				if !mtlsVal["enabled"].(bool) && !mtlsVal["auto"].(bool) {
					return false
				}
			}
		}
	}

	return true
}

func (c *Config) IstioOperator() string {
	data := ""
	if c.ControlPlaneValues != "" {
		data = Indent(c.ControlPlaneValues, "  ")
	} else if c.ValuesFile != "" {
		valfile, err := file.AsString(filepath.Join(c.ChartDir, c.ValuesFile))
		if err != nil {
			return ""
		}
		data = fmt.Sprintf(`
  values:
%s`, Indent(valfile, "    "))
	}

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return ""
	}

	return fmt.Sprintf(`
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  hub: %s
  tag: %s
%s
`, s.Hub, s.Tag, data)
}

// indents a block of text with an indent string
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

	if err := normalizeFile(&s.ChartDir); err != nil {
		return Config{}, err
	}

	if err := checkFileExists(filepath.Join(s.ChartDir, s.ValuesFile)); err != nil {
		return Config{}, err
	}

	if err := normalizeFile(&s.CrdsFilesDir); err != nil {
		return Config{}, err
	}

	deps, err := image.SettingsFromCommandLine()
	if err != nil {
		return Config{}, err
	}

	if s.Values, err = newHelmValues(ctx, deps); err != nil {
		return Config{}, err
	}

	if ctx.Settings().CIMode {
		s.DeployTimeout = DefaultCIDeployTimeout
		s.UndeployTimeout = DefaultCIUndeployTimeout
	} else {
		s.DeployTimeout = DefaultDeployTimeout
		s.UndeployTimeout = DefaultUndeployTimeout
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

func normalizeFile(path *string) error {
	// If the path uses the homedir ~, expand the path.
	var err error
	*path, err = homedir.Expand(*path)
	if err != nil {
		return err
	}

	return checkFileExists(*path)
}

func checkFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
}

func newHelmValues(ctx resource.Context, s *image.Settings) (map[string]string, error) {
	userValues, err := parseHelmValues()
	if err != nil {
		return nil, err
	}

	// Copy the defaults first.
	values := make(map[string]string)

	// Common values
	values[image.HubValuesKey] = s.Hub
	values[image.TagValuesKey] = s.Tag
	values[image.ImagePullPolicyValuesKey] = s.PullPolicy

	// Copy the user values.
	for k, v := range userValues {
		values[k] = v
	}

	// Always pull Docker images if using the "latest".
	if values[image.TagValuesKey] == image.LatestTag {
		values[image.ImagePullPolicyValuesKey] = string(kubeCore.PullAlways)
	}

	// We need more information on Envoy logs to detect usage of any deprecated feature
	if ctx.Settings().FailOnDeprecation {
		values["global.proxy.logLevel"] = "debug"
		values["global.proxy.componentLogLevel"] = "misc:debug"
	}

	return values, nil
}

func parseHelmValues() (map[string]string, error) {
	out := make(map[string]string)
	if helmValues == "" {
		return out, nil
	}

	values := strings.Split(helmValues, ",")
	for _, v := range values {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing helm values: %s", helmValues)
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
}

// String implements fmt.Stringer
func (c *Config) String() string {
	result := ""

	result += fmt.Sprintf("SystemNamespace:                %s\n", c.SystemNamespace)
	result += fmt.Sprintf("IstioNamespace:                 %s\n", c.IstioNamespace)
	result += fmt.Sprintf("ConfigNamespace:                %s\n", c.ConfigNamespace)
	result += fmt.Sprintf("TelemetryNamespace:             %s\n", c.TelemetryNamespace)
	result += fmt.Sprintf("PolicyNamespace:                %s\n", c.PolicyNamespace)
	result += fmt.Sprintf("IngressNamespace:               %s\n", c.IngressNamespace)
	result += fmt.Sprintf("EgressNamespace:                %s\n", c.EgressNamespace)
	result += fmt.Sprintf("DeployIstio:                    %v\n", c.DeployIstio)
	result += fmt.Sprintf("Operator:                       %v\n", c.Operator)
	result += fmt.Sprintf("DeployTimeout:                  %s\n", c.DeployTimeout.String())
	result += fmt.Sprintf("UndeployTimeout:                %s\n", c.UndeployTimeout.String())
	result += fmt.Sprintf("Values:                         %v\n", c.Values)
	result += fmt.Sprintf("ChartDir:                       %s\n", c.ChartDir)
	result += fmt.Sprintf("CrdsFilesDir:                   %s\n", c.CrdsFilesDir)
	result += fmt.Sprintf("ValuesFile:                     %s\n", c.ValuesFile)
	result += fmt.Sprintf("SkipWaitForValidationWebhook:   %v\n", c.SkipWaitForValidationWebhook)
	result += fmt.Sprintf("CustomSidecarInjectorNamespace: %s\n", c.CustomSidecarInjectorNamespace)

	return result
}

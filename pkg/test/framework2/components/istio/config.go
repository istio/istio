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

	"istio.io/istio/pkg/test/framework2/components/environment/kube"

	"istio.io/istio/pkg/test/framework2/core"

	yaml2 "gopkg.in/yaml.v2"

	"istio.io/istio/pkg/test"

	"github.com/mitchellh/go-homedir"

	kubeCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/env"
)

const (
	// DefaultSystemNamespace default value for SystemNamespace
	DefaultSystemNamespace = "istio-system"

	// ValuesMcpFile for Istio Helm deployment.
	E2EValuesFile = "values-e2e.yaml"

	// DefaultDeployTimeout for Istio
	DefaultDeployTimeout = time.Second * 300

	// DefaultCIDeployTimeout for Istio
	DefaultCIDeployTimeout = time.Second * 600

	// DefaultUndeployTimeout for Istio.
	DefaultUndeployTimeout = time.Second * 300

	// DefaultCIUndeployTimeout for Istio.
	DefaultCIUndeployTimeout = time.Second * 600

	// DefaultIstioChartRepo for Istio.
	DefaultIstioChartRepo = "https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/release-1.1-latest-daily/charts/"
)

var (
	helmValues string

	settingsFromCommandline = &Config{
		ChartRepo:       DefaultIstioChartRepo,
		SystemNamespace: DefaultSystemNamespace,
		DeployIstio:     true,
		DeployTimeout:   0,
		UndeployTimeout: 0,
		ChartDir:        env.IstioChartDir,
		CrdsFilesDir:    env.CrdsFilesDir,
		ValuesFile:      E2EValuesFile,
	}

	// Defaults for helm overrides.
	defaultHelmValues = map[string]string{
		kube.HubValuesKey:             kube.HUB.Value(),
		kube.TagValuesKey:             kube.TAG.Value(),
		"global.proxy.enableCoreDump": "true",
		"global.mtls.enabled":         "true",
		"galley.enabled":              "true",
	}
)

// Config provide kube-specific Config from flags.
type Config struct {
	// The namespace where the Istio components reside in a typical deployment (default: "istio-system").
	SystemNamespace string

	// Indicates that the test should deploy Istio into the target Kubernetes cluster before running tests.
	DeployIstio bool

	// DeployTimeout the timeout for deploying Istio.
	DeployTimeout time.Duration

	// UndeployTimeout the timeout for undeploying Istio.
	UndeployTimeout time.Duration

	ChartRepo string

	// The top-level Helm chart dir.
	ChartDir string

	// The top-level Helm Crds files dir.
	CrdsFilesDir string

	// The Helm values file to be used.
	ValuesFile string

	// Overrides for the Helm values file.
	Values map[string]string
}

// Is mtls enabled. Check in Values flag and Values file.
func (c *Config) IsMtlsEnabled() bool {
	if c.Values["global.mtls.enabled"] == "true" {
		return true
	}

	data, err := test.ReadConfigFile(filepath.Join(c.ChartDir, c.ValuesFile))
	if err != nil {
		return false
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
				return mtlsVal["enabled"].(bool)
			}
		}
	}

	return false
}

// DefaultConfig creates a new Config from defaults, environments variables, and command-line parameters.
func DefaultConfig(ctx core.Context) (Config, error) {
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

	var err error
	s.Values, err = newHelmValues()
	if err != nil {
		return Config{}, err
	}

	if ctx.Settings().CIMode {
		s.DeployTimeout = DefaultDeployTimeout
		s.UndeployTimeout = DefaultUndeployTimeout
	} else {
		s.DeployTimeout = DefaultCIDeployTimeout
		s.UndeployTimeout = DefaultCIUndeployTimeout
	}

	return s, nil
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

func newHelmValues() (map[string]string, error) {
	userValues, err := parseHelmValues()
	if err != nil {
		return nil, err
	}

	// Copy the defaults first.
	values := make(map[string]string)
	for k, v := range defaultHelmValues {
		values[k] = v
	}
	// Copy the user values.
	for k, v := range userValues {
		values[k] = v
	}
	// Always pull Docker images if using the "latest".
	if values[kube.TagValuesKey] == kube.LatestTag {
		values[kube.ImagePullPolicyValuesKey] = string(kubeCore.PullAlways)
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

	result += fmt.Sprintf("SystemNamespace: %s\n", c.SystemNamespace)
	result += fmt.Sprintf("DeployIstio:     %v\n", c.DeployIstio)
	result += fmt.Sprintf("DeployTimeout:   %s\n", c.DeployTimeout.String())
	result += fmt.Sprintf("UndeployTimeout: %s\n", c.UndeployTimeout.String())
	result += fmt.Sprintf("Values:          %v\n", c.Values)

	return result
}

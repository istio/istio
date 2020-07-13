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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/vfs"
)

const (
	// DefaultProfileFilename is the name of the default profile yaml file.
	DefaultProfileFilename = "default.yaml"

	ChartsSubdirName = "charts"
	profilesRoot     = "profiles"
)

// VFSRenderer is a helm template renderer that uses compiled-in helm charts.
type VFSRenderer struct {
	namespace        string
	componentName    string
	helmChartDirPath string
	chart            *chart.Chart
	started          bool
}

// NewVFSRenderer creates a VFSRenderer with the given relative path to helm charts, component name and namespace and
// a base values YAML string.
func NewVFSRenderer(helmChartDirPath, componentName, namespace string) *VFSRenderer {
	scope.Debugf("NewVFSRenderer with helmChart=%s, componentName=%s, namespace=%s", helmChartDirPath, componentName, namespace)
	return &VFSRenderer{
		namespace:        namespace,
		componentName:    componentName,
		helmChartDirPath: helmChartDirPath,
	}
}

// Run implements the TemplateRenderer interface.
func (h *VFSRenderer) Run() error {
	if err := CheckCompiledInCharts(); err != nil {
		return err
	}
	scope.Debugf("Run VFSRenderer with helmChart=%s, componentName=%s, namespace=%s", h.helmChartDirPath, h.componentName, h.namespace)
	if err := h.loadChart(); err != nil {
		return err
	}
	h.started = true
	return nil
}

// RenderManifest renders the current helm templates with the current values and returns the resulting YAML manifest
// string.
func (h *VFSRenderer) RenderManifest(values string) (string, error) {
	if !h.started {
		return "", fmt.Errorf("VFSRenderer for %s not started in renderChart", h.componentName)
	}
	return renderChart(h.namespace, values, h.chart)
}

// LoadValuesVFS loads the compiled in file corresponding to the given profile name.
func LoadValuesVFS(profileName string) (string, error) {
	if err := CheckCompiledInCharts(); err != nil {
		return "", err
	}
	path := filepath.Join(profilesRoot, BuiltinProfileToFilename(profileName))
	scope.Infof("Loading values from compiled in VFS at path %s", path)
	b, err := vfs.ReadFile(path)
	return string(b), err
}

func LoadValues(profileName string, chartsDir string) (string, error) {
	path := filepath.Join(chartsDir, profilesRoot, BuiltinProfileToFilename(profileName))
	scope.Infof("Loading values at path %s", path)
	b, err := ioutil.ReadFile(path)
	return string(b), err
}

func readProfiles(chartsDir string) (map[string]bool, error) {
	profiles := map[string]bool{}
	switch chartsDir {
	case "":
		if err := CheckCompiledInCharts(); err != nil {
			return nil, err
		}
		profilePaths, err := vfs.ReadDir(filepath.Join(chartsDir, profilesRoot))
		if err != nil {
			return nil, fmt.Errorf("failed to read profiles: %v", err)
		}
		for _, f := range profilePaths {
			profiles[strings.TrimSuffix(f, ".yaml")] = true
		}
	default:
		dir, err := ioutil.ReadDir(filepath.Join(chartsDir, profilesRoot))
		if err != nil {
			return nil, fmt.Errorf("failed to read profiles: %v", err)
		}
		for _, f := range dir {
			profiles[strings.TrimSuffix(f.Name(), ".yaml")] = true
		}
	}
	return profiles, nil
}

// loadChart implements the TemplateRenderer interface.
func (h *VFSRenderer) loadChart() error {
	prefix := h.helmChartDirPath
	fnames, err := vfs.GetFilesRecursive(prefix)
	if err != nil {
		return err
	}
	var bfs []*loader.BufferedFile
	for _, fname := range fnames {
		b, err := vfs.ReadFile(fname)
		if err != nil {
			return err
		}
		// Helm expects unix / separator, but on windows this will be \
		name := strings.ReplaceAll(stripPrefix(fname, prefix), string(filepath.Separator), "/")
		bf := &loader.BufferedFile{
			Name: name,
			Data: b,
		}
		bfs = append(bfs, bf)
		scope.Debugf("Chart loaded: %s", bf.Name)
	}

	h.chart, err = loader.LoadFiles(bfs)
	return err
}

func BuiltinProfileToFilename(name string) string {
	if name == "" {
		return DefaultProfileFilename
	}
	return name + ".yaml"
}

// stripPrefix removes the the given prefix from prefix.
func stripPrefix(path, prefix string) string {
	pl := len(strings.Split(prefix, string(filepath.Separator)))
	pv := strings.Split(path, string(filepath.Separator))
	return strings.Join(pv[pl:], string(filepath.Separator))
}

// list all the profiles.
func ListProfiles(charts string) ([]string, error) {
	profiles, err := readProfiles(charts)
	if err != nil {
		return nil, err
	}
	return util.StringBoolMapToSlice(profiles), nil
}

// CheckCompiledInCharts tests for the presence of compiled in charts. These can be missing if a developer creates
// binaries using go build instead of make and tries to use compiled in charts.
func CheckCompiledInCharts() error {
	if _, err := vfs.Stat(ChartsSubdirName); err != nil {
		return fmt.Errorf("compiled in charts not found in this development build, use --manifests with " +
			"local charts instead (e.g. istioctl install --manifests manifests/) or run make gen-charts and rebuild istioctl")
	}
	return nil
}

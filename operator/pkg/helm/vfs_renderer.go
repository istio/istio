// Copyright 2019 Istio Authors
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
	"path/filepath"
	"strings"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"

	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/vfs"

	"istio.io/pkg/log"
)

const (
	// DefaultProfileFilename is the name of the default profile yaml file.
	DefaultProfileFilename = "default.yaml"

	chartsRoot   = "charts"
	profilesRoot = "profiles"
)

var (
	// ProfileNames holds the names of all the profiles in the /profiles directory, without .yaml suffix.
	ProfileNames = make(map[string]bool)
)

func init() {
	profilePaths, err := vfs.ReadDir(profilesRoot)
	if err != nil {
		panic(err)
	}
	for _, p := range profilePaths {
		p = strings.TrimSuffix(p, ".yaml")
		ProfileNames[p] = true
	}
}

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
	log.Debugf("NewVFSRenderer with helmChart=%s, componentName=%s, namespace=%s", helmChartDirPath, componentName, namespace)
	return &VFSRenderer{
		namespace:        namespace,
		componentName:    componentName,
		helmChartDirPath: helmChartDirPath,
	}
}

// Run implements the TemplateRenderer interface.
func (h *VFSRenderer) Run() error {
	log.Debugf("Run VFSRenderer with helmChart=%s, componentName=%s, namespace=%s", h.helmChartDirPath, h.componentName, h.namespace)
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
	path := filepath.Join(profilesRoot, BuiltinProfileToFilename(profileName))
	log.Infof("Loading values from compiled in VFS at path %s", path)
	b, err := vfs.ReadFile(path)
	return string(b), err
}

func isBuiltinProfileName(name string) bool {
	if name == "" {
		return true
	}
	return ProfileNames[name]
}

// loadChart implements the TemplateRenderer interface.
func (h *VFSRenderer) loadChart() error {
	prefix := filepath.Join(chartsRoot, h.helmChartDirPath)
	fnames, err := vfs.GetFilesRecursive(prefix)
	if err != nil {
		return err
	}
	var bfs []*chartutil.BufferedFile
	for _, fname := range fnames {
		b, err := vfs.ReadFile(fname)
		if err != nil {
			return err
		}
		// Helm expects unix / separator, but on windows this will be \
		name := strings.ReplaceAll(stripPrefix(fname, prefix), string(filepath.Separator), "/")
		bf := &chartutil.BufferedFile{
			Name: name,
			Data: b,
		}
		bfs = append(bfs, bf)
		log.Debugf("Chart loaded: %s", bf.Name)
	}

	h.chart, err = chartutil.LoadFiles(bfs)
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

// list all the builtin profiles.
func ListBuiltinProfiles() []string {
	return util.StringBoolMapToSlice(ProfileNames)
}

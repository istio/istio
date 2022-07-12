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
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/util"
)

const (
	// DefaultProfileFilename is the name of the default profile yaml file.
	DefaultProfileFilename = "default.yaml"
	ChartsSubdirName       = "charts"
	profilesRoot           = "profiles"
)

// Renderer is a helm template renderer for a fs.FS.
type Renderer struct {
	namespace     string
	componentName string
	chart         *chart.Chart
	started       bool
	files         fs.FS
	dir           string
	// Kubernetes cluster version
	version *version.Info
}

// NewFileTemplateRenderer creates a TemplateRenderer with the given parameters and returns a pointer to it.
// helmChartDirPath must be an absolute file path to the root of the helm charts.
func NewGenericRenderer(files fs.FS, dir, componentName, namespace string, version *version.Info) *Renderer {
	return &Renderer{
		namespace:     namespace,
		componentName: componentName,
		dir:           dir,
		files:         files,
		version:       version,
	}
}

// Run implements the TemplateRenderer interface.
func (h *Renderer) Run() error {
	if err := h.loadChart(); err != nil {
		return err
	}

	h.started = true
	return nil
}

// RenderManifest renders the current helm templates with the current values and returns the resulting YAML manifest string.
func (h *Renderer) RenderManifest(values string) (string, error) {
	if !h.started {
		return "", fmt.Errorf("fileTemplateRenderer for %s not started in renderChart", h.componentName)
	}
	return renderChart(h.namespace, values, h.chart, nil, h.version)
}

// RenderManifestFiltered filters templates to render using the supplied filter function.
func (h *Renderer) RenderManifestFiltered(values string, filter TemplateFilterFunc) (string, error) {
	if !h.started {
		return "", fmt.Errorf("fileTemplateRenderer for %s not started in renderChart", h.componentName)
	}
	return renderChart(h.namespace, values, h.chart, filter, h.version)
}

func GetFilesRecursive(f fs.FS, root string) ([]string, error) {
	res := []string{}
	err := fs.WalkDir(f, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		res = append(res, path)
		return nil
	})
	return res, err
}

// loadChart implements the TemplateRenderer interface.
func (h *Renderer) loadChart() error {
	fnames, err := GetFilesRecursive(h.files, h.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("component %q does not exist", h.componentName)
		}
		return fmt.Errorf("list files: %v", err)
	}
	var bfs []*loader.BufferedFile
	for _, fname := range fnames {
		b, err := fs.ReadFile(h.files, fname)
		if err != nil {
			return fmt.Errorf("read file: %v", err)
		}
		// Helm expects unix / separator, but on windows this will be \
		name := strings.ReplaceAll(stripPrefix(fname, h.dir), string(filepath.Separator), "/")
		bf := &loader.BufferedFile{
			Name: name,
			Data: b,
		}
		bfs = append(bfs, bf)
		scope.Debugf("Chart loaded: %s", bf.Name)
	}

	h.chart, err = loader.LoadFiles(bfs)
	if err != nil {
		return fmt.Errorf("load files: %v", err)
	}
	return nil
}

func builtinProfileToFilename(name string) string {
	if name == "" {
		return DefaultProfileFilename
	}
	return name + ".yaml"
}

func LoadValues(profileName string, chartsDir string) (string, error) {
	path := strings.Join([]string{profilesRoot, builtinProfileToFilename(profileName)}, "/")
	by, err := fs.ReadFile(manifests.BuiltinOrDir(chartsDir), path)
	if err != nil {
		return "", err
	}
	return string(by), nil
}

func readProfiles(chartsDir string) (map[string]bool, error) {
	profiles := map[string]bool{}
	f := manifests.BuiltinOrDir(chartsDir)
	dir, err := fs.ReadDir(f, profilesRoot)
	if err != nil {
		return nil, err
	}
	for _, f := range dir {
		trimmedString := strings.TrimSuffix(f.Name(), ".yaml")
		if f.Name() != trimmedString {
			profiles[trimmedString] = true
		}
	}
	return profiles, nil
}

// stripPrefix removes the the given prefix from prefix.
func stripPrefix(path, prefix string) string {
	pl := len(strings.Split(prefix, "/"))
	pv := strings.Split(path, "/")
	return strings.Join(pv[pl:], "/")
}

// list all the profiles.
func ListProfiles(charts string) ([]string, error) {
	profiles, err := readProfiles(charts)
	if err != nil {
		return nil, err
	}
	return util.StringBoolMapToSlice(profiles), nil
}

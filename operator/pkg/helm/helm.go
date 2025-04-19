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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/yml"
)

// Render produces a set of fully rendered manifests from Helm.
// Any warnings are also propagated up.
// Note: this is the direct result of the Helm call. Postprocessing steps are done later.
// TODO: ReleaseName hardcoded to istio, this should probably have been the component name. However, its a bit late to change it.
func Render(releaseName, namespace string, directory string, iop values.Map, kubernetesVersion *version.Info) ([]manifest.Manifest, util.Errors, error) {
	vals, _ := iop.GetPathMap("spec.values")
	installPackagePath := iop.GetPathString("spec.installPackagePath")
	f := manifests.BuiltinOrDir(installPackagePath)
	path := pathJoin("charts", directory)
	chrt, err := loadChart(f, path)
	if err != nil {
		return nil, nil, fmt.Errorf("load chart: %v", err)
	}

	output, warnings, err := renderChart(releaseName, namespace, vals, chrt, kubernetesVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("render chart: %v", err)
	}
	mfs, err := manifest.Parse(output)
	return mfs, warnings, err
}

// TemplateFilterFunc filters templates to render by their file name
type TemplateFilterFunc func(string) bool

type Warnings = util.Errors

// renderChart renders the given chart with the given values and returns the resulting YAML manifest string.
func renderChart(releaseName string, namespace string, vals values.Map, chrt *chart.Chart, version *version.Info) ([]string, Warnings, error) {
	options := chartutil.ReleaseOptions{
		Name:      releaseName,
		Namespace: namespace,
	}

	caps := *chartutil.DefaultCapabilities

	// overwrite helm default capabilities
	operatorVersion, _ := chartutil.ParseKubeVersion("1." + strconv.Itoa(k8sversion.MinK8SVersion) + ".0")
	caps.KubeVersion = *operatorVersion

	if version != nil {
		caps.KubeVersion = chartutil.KubeVersion{
			Version: version.GitVersion,
			Major:   version.Major,
			Minor:   version.Minor,
		}
	}
	helmVals, err := chartutil.ToRenderValues(chrt, vals, options, &caps)
	if err != nil {
		return nil, nil, fmt.Errorf("converting values: %v", err)
	}

	files, err := engine.Render(chrt, helmVals)
	if err != nil {
		return nil, nil, err
	}

	crdFiles := chrt.CRDObjects()
	if chrt.Metadata.Name == "base" {
		enableIstioConfigCRDs, ok := values.GetPathAs[bool](vals, "base.enableIstioConfigCRDs")
		if ok && !enableIstioConfigCRDs {
			crdFiles = []chart.CRD{}
		}
	}

	var warnings Warnings
	// Create sorted array of keys to iterate over, to stabilize the order of the rendered templates
	keys := make([]string, 0, len(files))
	for k, v := range files {
		if strings.HasSuffix(k, NotesFileNameSuffix) {
			warnings = extractWarnings(v)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	results := make([]string, 0, len(keys))
	for _, k := range keys {
		results = append(results, yml.SplitString(files[k])...)
	}

	// Sort crd files by name to ensure stable manifest output
	slices.SortBy(crdFiles, func(a chart.CRD) string {
		return a.Name
	})
	for _, crd := range crdFiles {
		results = append(results, yml.SplitString(string(crd.File.Data))...)
	}

	return results, warnings, nil
}

func extractWarnings(v string) Warnings {
	var w Warnings
	for _, l := range strings.Split(v, "\n") {
		if strings.HasPrefix(l, "WARNING: ") {
			w = util.AppendErr(w, errors.New(strings.TrimPrefix(l, "WARNING: ")))
		}
	}
	return w
}

const (
	// NotesFileNameSuffix is the file name suffix for helm notes.
	// see https://helm.sh/docs/chart_template_guide/notes_files/
	NotesFileNameSuffix = ".txt"
)

// loadChart reads a chart from the filesystem. This is like loader.LoadDir but allows a fs.FS.
func loadChart(f fs.FS, root string) (*chart.Chart, error) {
	fnames, err := getFilesRecursive(f, root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("component does not exist")
		}
		return nil, fmt.Errorf("list files: %v", err)
	}
	var bfs []*loader.BufferedFile
	for _, fname := range fnames {
		b, err := fs.ReadFile(f, fname)
		if err != nil {
			return nil, fmt.Errorf("read file: %v", err)
		}
		// Helm expects unix / separator, but on windows this will be \
		name := strings.ReplaceAll(stripPrefix(fname, root), string(filepath.Separator), "/")
		bf := &loader.BufferedFile{
			Name: name,
			Data: b,
		}
		bfs = append(bfs, bf)
	}

	return loader.LoadFiles(bfs)
}

// stripPrefix removes the given prefix from prefix.
func stripPrefix(path, prefix string) string {
	pl := len(strings.Split(prefix, "/"))
	pv := strings.Split(path, "/")
	return strings.Join(pv[pl:], "/")
}

func getFilesRecursive(f fs.FS, root string) ([]string, error) {
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

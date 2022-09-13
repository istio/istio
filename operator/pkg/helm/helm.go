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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/yaml"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	// YAMLSeparator is a separator for multi-document YAML files.
	YAMLSeparator = "\n---\n"

	// DefaultProfileString is the name of the default profile.
	DefaultProfileString = "default"

	// NotesFileNameSuffix is the file name suffix for helm notes.
	// see https://helm.sh/docs/chart_template_guide/notes_files/
	NotesFileNameSuffix = ".txt"
)

var scope = log.RegisterScope("installer", "installer", 0)

// TemplateFilterFunc filters templates to render by their file name
type TemplateFilterFunc func(string) bool

// TemplateRenderer defines a helm template renderer interface.
type TemplateRenderer interface {
	// Run starts the renderer and should be called before using it.
	Run() error
	// RenderManifest renders the associated helm charts with the given values YAML string and returns the resulting
	// string.
	RenderManifest(values string) (string, error)
	// RenderManifestFiltered filters manifests to render by template file name
	RenderManifestFiltered(values string, filter TemplateFilterFunc) (string, error)
}

// NewHelmRenderer creates a new helm renderer with the given parameters and returns an interface to it.
// The format of helmBaseDir and profile strings determines the type of helm renderer returned (compiled-in, file,
// HTTP etc.)
func NewHelmRenderer(operatorDataDir, helmSubdir, componentName, namespace string, version *version.Info) TemplateRenderer {
	dir := strings.Join([]string{ChartsSubdirName, helmSubdir}, "/")
	return NewGenericRenderer(manifests.BuiltinOrDir(operatorDataDir), dir, componentName, namespace, version)
}

// ReadProfileYAML reads the YAML values associated with the given profile. It uses an appropriate reader for the
// profile format (compiled-in, file, HTTP, etc.).
func ReadProfileYAML(profile, manifestsPath string) (string, error) {
	var err error
	var globalValues string

	// Get global values from profile.
	switch {
	case util.IsFilePath(profile):
		if globalValues, err = readFile(profile); err != nil {
			return "", err
		}
	default:
		if globalValues, err = LoadValues(profile, manifestsPath); err != nil {
			return "", fmt.Errorf("failed to read profile %v from %v: %v", profile, manifestsPath, err)
		}
	}

	return globalValues, nil
}

// renderChart renders the given chart with the given values and returns the resulting YAML manifest string.
func renderChart(namespace, values string, chrt *chart.Chart, filterFunc TemplateFilterFunc, version *version.Info) (string, error) {
	options := chartutil.ReleaseOptions{
		Name:      "istio",
		Namespace: namespace,
	}
	valuesMap := map[string]any{}
	if err := yaml.Unmarshal([]byte(values), &valuesMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal values: %v", err)
	}

	caps := *chartutil.DefaultCapabilities
	if version != nil {
		caps.KubeVersion = chartutil.KubeVersion{
			Version: version.GitVersion,
			Major:   version.Major,
			Minor:   version.Minor,
		}
	}
	vals, err := chartutil.ToRenderValues(chrt, valuesMap, options, &caps)
	if err != nil {
		return "", err
	}

	if filterFunc != nil {
		filteredTemplates := []*chart.File{}
		for _, t := range chrt.Templates {
			if filterFunc(t.Name) {
				filteredTemplates = append(filteredTemplates, t)
			}
		}
		chrt.Templates = filteredTemplates
	}

	files, err := engine.Render(chrt, vals)
	crdFiles := chrt.CRDObjects()
	if err != nil {
		return "", err
	}
	if chrt.Metadata.Name == "base" {
		base, _ := valuesMap["base"].(map[string]any)
		if enableIstioConfigCRDs, ok := base["enableIstioConfigCRDs"].(bool); ok && !enableIstioConfigCRDs {
			crdFiles = []chart.CRD{}
		}
	}

	// Create sorted array of keys to iterate over, to stabilize the order of the rendered templates
	keys := make([]string, 0, len(files))
	for k := range files {
		if strings.HasSuffix(k, NotesFileNameSuffix) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i := 0; i < len(keys); i++ {
		f := files[keys[i]]
		// add yaml separator if the rendered file doesn't have one at the end
		f = strings.TrimSpace(f) + "\n"
		if !strings.HasSuffix(f, YAMLSeparator) {
			f += YAMLSeparator
		}
		_, err := sb.WriteString(f)
		if err != nil {
			return "", err
		}
	}

	// Sort crd files by name to ensure stable manifest output
	sort.Slice(crdFiles, func(i, j int) bool { return crdFiles[i].Name < crdFiles[j].Name })
	for _, crdFile := range crdFiles {
		f := string(crdFile.File.Data)
		// add yaml separator if the rendered file doesn't have one at the end
		f = strings.TrimSpace(f) + "\n"
		if !strings.HasSuffix(f, YAMLSeparator) {
			f += YAMLSeparator
		}
		_, err := sb.WriteString(f)
		if err != nil {
			return "", err
		}
	}

	return sb.String(), nil
}

// GenerateHubTagOverlay creates an IstioOperatorSpec overlay YAML for hub and tag.
func GenerateHubTagOverlay(hub, tag string) (string, error) {
	hubTagYAMLTemplate := `
spec:
  hub: {{.Hub}}
  tag: {{.Tag}}
`
	ts := struct {
		Hub string
		Tag string
	}{
		Hub: hub,
		Tag: tag,
	}
	return util.RenderTemplate(hubTagYAMLTemplate, ts)
}

// DefaultFilenameForProfile returns the profile name of the default profile for the given profile.
func DefaultFilenameForProfile(profile string) string {
	switch {
	case util.IsFilePath(profile):
		return filepath.Join(filepath.Dir(profile), DefaultProfileFilename)
	default:
		return DefaultProfileString
	}
}

// IsDefaultProfile reports whether the given profile is the default profile.
func IsDefaultProfile(profile string) bool {
	return profile == "" || profile == DefaultProfileString || filepath.Base(profile) == DefaultProfileFilename
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// GetProfileYAML returns the YAML for the given profile name, using the given profileOrPath string, which may be either
// a profile label or a file path.
func GetProfileYAML(installPackagePath, profileOrPath string) (string, error) {
	if profileOrPath == "" {
		profileOrPath = "default"
	}
	profiles, err := readProfiles(installPackagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read profiles: %v", err)
	}
	// If charts are a file path and profile is a name like default, transform it to the file path.
	if profiles[profileOrPath] && installPackagePath != "" {
		profileOrPath = filepath.Join(installPackagePath, "profiles", profileOrPath+".yaml")
	}
	// This contains the IstioOperator CR.
	baseCRYAML, err := ReadProfileYAML(profileOrPath, installPackagePath)
	if err != nil {
		return "", err
	}

	if !IsDefaultProfile(profileOrPath) {
		// Profile definitions are relative to the default profileOrPath, so read that first.
		dfn := DefaultFilenameForProfile(profileOrPath)
		defaultYAML, err := ReadProfileYAML(dfn, installPackagePath)
		if err != nil {
			return "", err
		}
		baseCRYAML, err = util.OverlayIOP(defaultYAML, baseCRYAML)
		if err != nil {
			return "", err
		}
	}

	return baseCRYAML, nil
}

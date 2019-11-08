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
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"

	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	// YAMLSeparator is a separator for multi-document YAML files.
	YAMLSeparator = "\n---\n"

	// DefaultProfileString is the name of the default profile.
	DefaultProfileString = "default"

	// notes file name suffix for the helm chart.
	NotesFileNameSuffix = ".txt"
)

// TemplateRenderer defines a helm template renderer interface.
type TemplateRenderer interface {
	// Run starts the renderer and should be called before using it.
	Run() error
	// RenderManifest renders the associated helm charts with the given values YAML string and returns the resulting
	// string.
	RenderManifest(values string) (string, error)
}

// NewHelmRenderer creates a new helm renderer with the given parameters and returns an interface to it.
// The format of helmBaseDir and profile strings determines the type of helm renderer returned (compiled-in, file,
// HTTP etc.)
func NewHelmRenderer(chartsRootDir, helmBaseDir, componentName, namespace string) (TemplateRenderer, error) {
	// filepath would remove leading slash here if chartsRootDir is empty.
	dir := chartsRootDir + "/" + helmBaseDir
	switch {
	case chartsRootDir == "":
		return NewVFSRenderer(helmBaseDir, componentName, namespace), nil
	case util.IsFilePath(dir):
		return NewFileTemplateRenderer(dir, componentName, namespace), nil
	default:
		return nil, fmt.Errorf("unknown helm renderer with chartsRoot=%s", chartsRootDir)
	}
}

// ReadProfileYAML reads the YAML values associated with the given profile. It uses an appropriate reader for the
// profile format (compiled-in, file, HTTP, etc.).
func ReadProfileYAML(profile string) (string, error) {
	var err error
	var globalValues string
	if profile == "" {
		log.Infof("ReadProfileYAML for profile name: [Empty]")
	} else {
		log.Infof("ReadProfileYAML for profile name: %s", profile)
	}

	// Get global values from profile.
	switch {
	case isBuiltinProfileName(profile):
		if globalValues, err = LoadValuesVFS(profile); err != nil {
			return "", err
		}
	case util.IsFilePath(profile):
		log.Infof("Loading values from local filesystem at path %s", profile)
		if globalValues, err = readFile(profile); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported Profile type: %s", profile)
	}

	return globalValues, nil
}

// renderChart renders the given chart with the given values and returns the resulting YAML manifest string.
func renderChart(namespace, values string, chrt *chart.Chart) (string, error) {
	config := &chart.Config{Raw: values, Values: map[string]*chart.Value{}}
	options := chartutil.ReleaseOptions{
		Name:      "istio",
		Time:      timeconv.Now(),
		Namespace: namespace,
	}

	vals, err := chartutil.ToRenderValuesCaps(chrt, config, options, nil)
	if err != nil {
		return "", err
	}

	files, err := engine.New().Render(chrt, vals)
	if err != nil {
		return "", err
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
		if !strings.HasSuffix(strings.TrimSpace(f)+"\n", YAMLSeparator) {
			f += YAMLSeparator
		}
		_, err := sb.WriteString(f)
		if err != nil {
			return "", err
		}
	}

	return sb.String(), nil
}

// OverlayYAML patches the overlay tree over the base tree and returns the result. All trees are expressed as YAML
// strings.
func OverlayYAML(base, overlay string) (string, error) {
	if overlay == "" {
		return base, nil
	}
	bj, err := yaml.YAMLToJSON([]byte(base))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in base: %s\n%s", err, bj)
	}
	oj, err := yaml.YAMLToJSON([]byte(overlay))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in overlay: %s\n%s", err, oj)
	}
	if overlay == "" {
		oj = []byte("{}")
	}

	merged, err := jsonpatch.MergePatch(bj, oj)
	if err != nil {
		return "", fmt.Errorf("json merge error (%s) for base object: \n%s\n override object: \n%s", err, bj, oj)
	}
	my, err := yaml.JSONToYAML(merged)
	if err != nil {
		return "", fmt.Errorf("jsonToYAML error (%s) for merged object: \n%s", err, merged)
	}

	return string(my), nil
}

// GenerateHubTagOverlay creates an IstioControlPlaneSpec overlay YAML for hub and tag.
func GenerateHubTagOverlay(hub, tag string) (string, error) {
	hubTagYAMLTemplate := `
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
	return renderTemplate(hubTagYAMLTemplate, ts)
}

// helper method to render template
func renderTemplate(tmpl string, ts interface{}) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = t.Execute(buf, ts)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// DefaultFilenameForProfile returns the profile name of the default profile for the given profile.
func DefaultFilenameForProfile(profile string) (string, error) {
	switch {
	case util.IsFilePath(profile):
		return filepath.Join(filepath.Dir(profile), DefaultProfileFilename), nil
	default:
		if _, ok := ProfileNames[profile]; ok || profile == "" {
			return DefaultProfileString, nil
		}
		return "", fmt.Errorf("bad profile string %s", profile)
	}
}

// IsDefaultProfile reports whether the given profile is the default profile.
func IsDefaultProfile(profile string) bool {
	return profile == "" || profile == DefaultProfileString || filepath.Base(profile) == DefaultProfileFilename
}

func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}

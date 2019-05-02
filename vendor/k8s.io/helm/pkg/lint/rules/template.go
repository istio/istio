/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/lint/support"
	cpb "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"
	tversion "k8s.io/helm/pkg/version"
)

// Templates lints the templates in the Linter.
func Templates(linter *support.Linter, values []byte, namespace string, strict bool) {
	path := "templates/"
	templatesPath := filepath.Join(linter.ChartDir, path)

	templatesDirExist := linter.RunLinterRule(support.WarningSev, path, validateTemplatesDir(templatesPath))

	// Templates directory is optional for now
	if !templatesDirExist {
		return
	}

	// Load chart and parse templates, based on tiller/release_server
	chart, err := chartutil.Load(linter.ChartDir)

	chartLoaded := linter.RunLinterRule(support.ErrorSev, path, err)

	if !chartLoaded {
		return
	}

	options := chartutil.ReleaseOptions{Name: "testRelease", Time: timeconv.Now(), Namespace: namespace}
	caps := &chartutil.Capabilities{
		APIVersions:   chartutil.DefaultVersionSet,
		KubeVersion:   chartutil.DefaultKubeVersion,
		TillerVersion: tversion.GetVersionProto(),
	}
	cvals, err := chartutil.CoalesceValues(chart, &cpb.Config{Raw: string(values)})
	if err != nil {
		return
	}
	// convert our values back into config
	yvals, err := cvals.YAML()
	if err != nil {
		return
	}
	cc := &cpb.Config{Raw: yvals}
	valuesToRender, err := chartutil.ToRenderValuesCaps(chart, cc, options, caps)
	if err != nil {
		// FIXME: This seems to generate a duplicate, but I can't find where the first
		// error is coming from.
		//linter.RunLinterRule(support.ErrorSev, err)
		return
	}
	e := engine.New()
	e.LintMode = true
	if strict {
		e.Strict = true
	}
	renderedContentMap, err := e.Render(chart, valuesToRender)

	renderOk := linter.RunLinterRule(support.ErrorSev, path, err)

	if !renderOk {
		return
	}

	/* Iterate over all the templates to check:
	- It is a .yaml file
	- All the values in the template file is defined
	- {{}} include | quote
	- Generated content is a valid Yaml file
	- Metadata.Namespace is not set
	*/
	for _, template := range chart.Templates {
		fileName, _ := template.Name, template.Data
		path = fileName

		linter.RunLinterRule(support.ErrorSev, path, validateAllowedExtension(fileName))

		// We only apply the following lint rules to yaml files
		if filepath.Ext(fileName) != ".yaml" || filepath.Ext(fileName) == ".yml" {
			continue
		}

		// NOTE: disabled for now, Refs https://github.com/kubernetes/helm/issues/1463
		// Check that all the templates have a matching value
		//linter.RunLinterRule(support.WarningSev, path, validateNoMissingValues(templatesPath, valuesToRender, preExecutedTemplate))

		// NOTE: disabled for now, Refs https://github.com/kubernetes/helm/issues/1037
		// linter.RunLinterRule(support.WarningSev, path, validateQuotes(string(preExecutedTemplate)))

		renderedContent := renderedContentMap[filepath.Join(chart.GetMetadata().Name, fileName)]
		var yamlStruct K8sYamlStruct
		// Even though K8sYamlStruct only defines Metadata namespace, an error in any other
		// key will be raised as well
		err := yaml.Unmarshal([]byte(renderedContent), &yamlStruct)

		validYaml := linter.RunLinterRule(support.ErrorSev, path, validateYamlContent(err))

		if !validYaml {
			continue
		}
	}
}

// Validation functions
func validateTemplatesDir(templatesPath string) error {
	if fi, err := os.Stat(templatesPath); err != nil {
		return errors.New("directory not found")
	} else if err == nil && !fi.IsDir() {
		return errors.New("not a directory")
	}
	return nil
}

func validateAllowedExtension(fileName string) error {
	ext := filepath.Ext(fileName)
	validExtensions := []string{".yaml", ".yml", ".tpl", ".txt"}

	for _, b := range validExtensions {
		if b == ext {
			return nil
		}
	}

	return fmt.Errorf("file extension '%s' not valid. Valid extensions are .yaml, .yml, .tpl, or .txt", ext)
}

func validateYamlContent(err error) error {
	if err != nil {
		return fmt.Errorf("unable to parse YAML\n\t%s", err)
	}
	return nil
}

// K8sYamlStruct stubs a Kubernetes YAML file.
// Need to access for now to Namespace only
type K8sYamlStruct struct {
	Metadata struct {
		Namespace string
	}
}

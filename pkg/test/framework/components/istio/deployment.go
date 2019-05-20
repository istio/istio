//  Copyright 2019 Istio Authors
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
	"io/ioutil"
	"path"
	"regexp"
	"sort"
	"strings"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/yml"
)

const (
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    istio-injection: disabled
`
)

func generateIstioYaml(helmDir string, cfg Config) (string, error) {
	generatedYaml, err := renderIstioTemplate(helmDir, cfg)
	if err != nil {
		return "", err
	}

	// TODO: This is Istio deployment specific. We may need to remove/reconcile this as a parameter
	// when we support Helm deployment of non-Istio artifacts.
	namespaceData := fmt.Sprintf(namespaceTemplate, cfg.SystemNamespace)
	generatedYaml = yml.JoinString(namespaceData, generatedYaml)

	return generatedYaml, nil
}

func renderIstioTemplate(helmDir string, cfg Config) (string, error) {
	if err := helm.Init(helmDir, true); err != nil {
		return "", err
	}

	renderedYaml, err := helm.Template(
		helmDir, cfg.ChartDir, "istio", cfg.SystemNamespace, path.Join(env.IstioChartDir, cfg.ValuesFile), cfg.Values)
	if err != nil {
		return "", err
	}

	return renderedYaml, nil
}

func splitIstioYaml(istioYaml string) (string, string) {
	allParts := yml.SplitString(istioYaml)
	installParts := make([]string, 0, len(allParts))
	configureParts := make([]string, 0, len(allParts))

	// Make the regular expression multi-line and anchor to the beginning of the line.
	r := regexp.MustCompile(`(?m)^apiVersion: *.*istio\.io.*`)

	for _, p := range allParts {
		if r.Match([]byte(p)) {
			configureParts = append(configureParts, p)
		} else {
			installParts = append(installParts, p)
		}
	}

	installYaml := yml.JoinString(installParts...)
	configureYaml := yml.JoinString(configureParts...)

	return installYaml, configureYaml
}

func generateCRDYaml(crdFilesDir string) (string, error) {

	// Note: When adding a CRD to the install, a new CRDFile* constant is needed
	// This slice contains the list of CRD files installed during testing
	var crdFiles []string
	fs, err := ioutil.ReadDir(crdFilesDir)
	if err != nil {
		return "", err
	}

	for _, f := range fs {
		if strings.HasPrefix(f.Name(), "crd-") {
			crdFiles = append(crdFiles, f.Name())
		}
	}
	sort.Strings(crdFiles)

	// Get Joined Crds Yaml file
	parts := make([]string, 0, len(crdFiles))
	for _, yamlFileName := range crdFiles {
		content, err := file.AsString(path.Join(crdFilesDir, yamlFileName))
		if err != nil {
			return "", err
		}
		parts = append(parts, content)
	}

	prevContent := yml.JoinString(parts...)

	return prevContent, nil
}

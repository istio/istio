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
	"path"
	"regexp"
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework2/core"
	"istio.io/istio/pkg/test/helm"
)

const (
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    istio-injection: disabled
`
	zeroCRDInstallFile = "crd-10.yaml"
	oneCRDInstallFile  = "crd-11.yaml"
	twoCRDInstallFile  = "crd-certmanager-10.yaml"
)

const (
	yamlSeparator = "\n---\n"
)

func generateIstioYaml(helmDir string, s *Config, context core.Context) (string, error) {
	generatedYaml, err := renderIstioTemplate(helmDir, s)

	// TODO: This is Istio deployment specific. We may need to remove/reconcile this as a parameter
	// when we support Helm deployment of non-Istio artifacts.
	namespaceData := fmt.Sprintf(namespaceTemplate, s.SystemNamespace)
	crdsData, err := getCrdsYamlFiles(s.CrdsFilesDir)
	if err != nil {
		return "", err
	}

	generatedYaml = test.JoinConfigs(namespaceData, crdsData, generatedYaml)

	return generatedYaml, nil
}

func renderIstioTemplate(helmDir string, s *Config) (string, error) {
	if err := helm.Init(helmDir, true); err != nil {
		return "", err
	}

	if err := helm.RepoAdd(helmDir, "istio.io", s.ChartRepo); err != nil { // TODO: make this a parameter
		return "", err
	}

	if err := helm.DepUpdate(helmDir, env.IstioChartDir, true); err != nil {
		return "", err
	}

	renderedYaml, err := helm.Template(helmDir, s.ChartDir, "istio", s.SystemNamespace, path.Join(env.IstioChartDir, s.ValuesFile), s.Values)
	if err != nil {
		return "", err
	}

	// Replace IfNotPresent with Always, so that we always pull images
	renderedYaml = strings.Replace(renderedYaml, "IfNotPresent", "Always", -1)

	return renderedYaml, nil
}

func splitIstioYaml(istioYaml string) (string, string) {
	installYaml := ""
	configureYaml := ""

	parts := strings.Split(istioYaml, yamlSeparator)

	r, err := regexp.Compile("apiVersion: *.*istio\\.io.*")
	if err != nil {
		panic(err)
	}

	for _, p := range parts {
		if r.Match([]byte(p)) {
			if configureYaml != "" {
				configureYaml += yamlSeparator
			}
			configureYaml += p
		} else {
			if installYaml != "" {
				installYaml += yamlSeparator
			}
			installYaml += p
		}
	}

	return installYaml, configureYaml
}

func getCrdsYamlFiles(crdFilesDir string) (string, error) {
	// Note: When adding a CRD to the install, a new CRDFile* constant is needed
	// This slice contains the list of CRD files installed during testing
	istioCRDFileNames := []string{zeroCRDInstallFile, oneCRDInstallFile, twoCRDInstallFile}
	// Get Joined Crds Yaml file
	prevContent := ""
	for _, yamlFileName := range istioCRDFileNames {
		content, err := test.ReadConfigFile(path.Join(crdFilesDir, yamlFileName))
		if err != nil {
			return "", err
		}
		prevContent = test.JoinConfigs(content, prevContent)
	}
	return prevContent, nil
}

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

package mesh

import (
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
)

type operatorCommonArgs struct {
	// hub is the hub for the operator image.
	hub string
	// tag is the tag for the operator image.
	tag string
	// operatorNamespace is the namespace the operator controller is installed into.
	operatorNamespace string
	// istioNamespace is the namespace Istio is installed into.
	istioNamespace string
}

const (
	operatorResourceName = "istio-operator"
)

func isControllerInstalled(kubeconfig, context, operatorNamespace string) (bool, error) {
	return manifest.DeploymentExists(kubeconfig, context, operatorNamespace, operatorResourceName)
}

// chartsRootDir, helmBaseDir, componentName, namespace string) (Template, TemplateRenderer, error) {
func renderOperatorManifest(_ *rootArgs, ocArgs *operatorCommonArgs, _ *Logger) (string, string, error) {
	r, err := helm.NewHelmRenderer("", "istio-operator", istioControllerComponentName, ocArgs.operatorNamespace)
	if err != nil {
		return "", "", err
	}

	if err := r.Run(); err != nil {
		return "", "", err
	}

	tmpl := `
operatorNamespace: {{.OperatorNamespace}}
istioNamespace: {{.IstioNamespace}}
hub: {{.Hub}}
tag: {{.Tag}}
`

	tv := struct {
		OperatorNamespace string
		IstioNamespace    string
		Hub               string
		Tag               string
	}{
		OperatorNamespace: ocArgs.operatorNamespace,
		IstioNamespace:    ocArgs.istioNamespace,
		Hub:               ocArgs.hub,
		Tag:               ocArgs.tag,
	}
	vals, err := util.RenderTemplate(tmpl, tv)
	if err != nil {
		return "", "", err
	}
	manifest, err := r.RenderManifest(vals)
	return vals, manifest, err
}

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

package mesh

import (
	"context"

	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	//  Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
)

type operatorCommonArgs struct {
	// hub is the hub for the operator image.
	hub string
	// tag is the tag for the operator image.
	tag string
	// imagePullSecrets is an array of imagePullSecret to pull operator image from the private registry
	imagePullSecrets []string
	// operatorNamespace is the namespace the operator controller is installed into.
	operatorNamespace string
	// watchedNamespaces is the namespaces the operator controller watches, could be namespace list separated by comma.
	watchedNamespaces string
	// istioNamespace is deprecated, use watchedNamespaces instead.
	istioNamespace string
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
	// revision is the Istio control plane revision the command targets.
	revision string
	// outputFormat controls the format of operator dumps
	outputFormat string
}

const (
	operatorResourceName     = "istio-operator"
	operatorDefaultNamespace = "istio-operator"
	istioDefaultNamespace    = "istio-system"
)

// isControllerInstalled reports whether an operator deployment exists in the given namespace.
func isControllerInstalled(cs kubernetes.Interface, operatorNamespace string, revision string) (bool, error) {
	orn := operatorResourceName
	if revision != "" {
		orn += "-" + revision
	}
	return deploymentExists(cs, operatorNamespace, orn)
}

// renderOperatorManifest renders a manifest to install the operator with the given input arguments.
func renderOperatorManifest(_ *RootArgs, ocArgs *operatorCommonArgs) (string, string, error) {
	// If manifestsPath is a URL, fetch and extract it and continue with the local filesystem path instead.
	installPackagePath, _, err := manifest.RewriteURLToLocalInstallPath(ocArgs.manifestsPath, "" /*profileOrPath*/, false /*skipValidation */)
	if err != nil {
		return "", "", err
	}
	r := helm.NewHelmRenderer(installPackagePath, "istio-operator", string(name.IstioOperatorComponentName), ocArgs.operatorNamespace)

	if err := r.Run(); err != nil {
		return "", "", err
	}

	tmpl := `
istioNamespace: {{.IstioNamespace}}
watchedNamespaces: {{.WatchedNamespaces}}
hub: {{.Hub}}
tag: {{.Tag}}
{{- if .ImagePullSecrets }}
imagePullSecrets:
{{- range .ImagePullSecrets }}
- {{ . }}
{{- end }}
{{- end }}
revision: {{if .Revision }} {{.Revision}} {{else}} "" {{end}}
`

	tv := struct {
		IstioNamespace    string
		WatchedNamespaces string
		Hub               string
		Tag               string
		ImagePullSecrets  []string
		Revision          string
	}{
		IstioNamespace:    ocArgs.istioNamespace,
		WatchedNamespaces: ocArgs.watchedNamespaces,
		Hub:               ocArgs.hub,
		Tag:               ocArgs.tag,
		ImagePullSecrets:  ocArgs.imagePullSecrets,
		Revision:          ocArgs.revision,
	}
	vals, err := util.RenderTemplate(tmpl, tv)
	if err != nil {
		return "", "", err
	}
	manifest, err := r.RenderManifest(vals)
	return vals, manifest, err
}

// deploymentExists returns true if the given deployment in the namespace exists.
func deploymentExists(cs kubernetes.Interface, namespace, name string) (bool, error) {
	d, err := cs.AppsV1().Deployments(namespace).Get(context.TODO(), name, v12.GetOptions{})
	if err != nil {
		return false, err
	}
	return d != nil, nil
}

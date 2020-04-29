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
	"context"
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
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
	// charts is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	charts string
}

const (
	operatorResourceName     = "istio-operator"
	operatorDefaultNamespace = "istio-operator"
	istioDefaultNamespace    = "istio-system"
)

func isControllerInstalled(cs kubernetes.Interface, operatorNamespace string) (bool, error) {
	return DeploymentExists(cs, operatorNamespace, operatorResourceName)
}

// chartsRootDir, helmBaseDir, componentName, namespace string) (Template, TemplateRenderer, error) {
func renderOperatorManifest(_ *rootArgs, ocArgs *operatorCommonArgs, _ clog.Logger) (string, string, error) {
	installPackagePath := snapshotInstallPackageDir
	if ocArgs.charts != "" {
		installPackagePath = ocArgs.charts
	}
	r, err := helm.NewHelmRenderer(installPackagePath, "istio-operator", string(name.IstioOperatorComponentName), ocArgs.operatorNamespace)
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

func CreateNamespace(cs kubernetes.Interface, namespace string) error {
	if namespace == "" {
		// Setup default namespace
		namespace = istioDefaultNamespace
	}

	ns := &v1.Namespace{ObjectMeta: v12.ObjectMeta{
		Name: namespace,
		Labels: map[string]string{
			"istio-injection": "disabled",
		},
	}}
	_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, v12.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace %v: %v", namespace, err)
	}
	return nil
}

func DeleteNamespace(cs kubernetes.Interface, namespace string) error {
	return cs.CoreV1().Namespaces().Delete(context.TODO(), namespace, v12.DeleteOptions{})
}

func DeploymentExists(cs kubernetes.Interface, namespace, name string) (bool, error) {
	d, err := cs.AppsV1().Deployments(namespace).Get(context.TODO(), name, v12.GetOptions{})
	if err != nil {
		return false, err
	}
	return d != nil, nil
}

func InitK8SRestClient(kubeconfig, kubeContext string) (*rest.Config, *kubernetes.Clientset, error) {
	restConfig, err := defaultRestConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	return restConfig, clientset, nil
}

func defaultRestConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

// BuildClientConfig is a helper function that builds client config from a kubeconfig filepath.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	//Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. In cluster config if running in-cluster
	// 3. Config(s) in KUBECONFIG environment variable
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

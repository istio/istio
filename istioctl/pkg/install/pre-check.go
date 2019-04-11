// Copyright 2019 Istio Authors.
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

package install

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	authorizationapi "k8s.io/api/authorization/v1beta1"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"
)

const (
	minK8SVersion = "1.11"
)

var (
	clientExecFactory = createKubeClient
)

type preCheckClient struct {
	client *kubernetes.Clientset
}
type preCheckExecClient interface {
	getNameSpace(ns string) (*v1.Namespace, error)
	serverVersion() (*version.Info, error)
	checkAuthorization(s *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error)
	checkMutatingWebhook() error
}

func installPreCheck(istioNamespaceFlag *string, restClientGetter resource.RESTClientGetter, writer io.Writer) error {
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Checking the cluster to make sure it is ready for Istio installation...\n")
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Kubernetes-api\n")
	fmt.Fprintf(writer, "-----------------------\n")
	c, err := clientExecFactory(restClientGetter)
	if err != nil {
		return fmt.Errorf("failed to initialize the Kubernetes client: %v", err)
	}
	fmt.Fprintf(writer, "Can initialize the Kubernetes client.\n")
	v, err := c.serverVersion()
	if err != nil {
		return fmt.Errorf("failed to query the Kubernetes API Server: %v", err)
	}
	fmt.Fprintf(writer, "Can query the Kubernetes API Server.\n")

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Kubernetes-version\n")
	fmt.Fprintf(writer, "-----------------------\n")

	if !checkKubernetesVersion(v) {
		msg := fmt.Sprintf("The Kubernetes API version: %v is lower than the minimum version: "+minK8SVersion, v)
		return errors.New(msg)
	}
	fmt.Fprintf(writer, "Istio is compatible with Kubernetes: %v.\n", v)

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Istio-existence\n")
	fmt.Fprintf(writer, "-----------------------\n")
	ns, _ := c.getNameSpace(*istioNamespaceFlag)
	if ns != nil {
		msg := fmt.Sprintf("Istio cannot be installed because the Istio namespace '%v' is already in use", *istioNamespaceFlag)
		return errors.New(msg)
	}
	fmt.Fprintf(writer, "Istio will be installed in the %v namespace.\n", *istioNamespaceFlag)

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Kubernetes-setup\n")
	fmt.Fprintf(writer, "-----------------------\n")
	Resources := []struct {
		namespace string
		group     string
		version   string
		name      string
	}{
		{
			namespace: "",
			group:     "",
			version:   "v1",
			name:      "Namespace",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "ClusterRole",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "ClusterRoleBinding",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "apiextensions.k8s.io",
			version:   "v1beta1",
			name:      "CustomResourceDefinition",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "Role",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "ServiceAccount",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "Service",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "extensions",
			version:   "v1beta1",
			name:      "Deployments",
		},
		{
			namespace: *istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "ConfigMap",
		},
	}
	var errs error
	resourceNames := make([]string, 0)
	for _, r := range Resources {
		err = checkCanCreateResources(c, r.namespace, r.group, r.version, r.name)
		if err != nil {
			errs = multierror.Append(errs, err)
		}
		resourceNames = append(resourceNames, r.name)
	}
	if errs != nil {
		return errs
	}

	fmt.Fprintf(writer, "Can create necessary Kubernetes configurations: "+"%v. \n", strings.Join(resourceNames, ","))
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "SideCar-Injector\n")
	fmt.Fprintf(writer, "-----------------------\n")
	err = c.checkMutatingWebhook()
	if err != nil {
		fmt.Fprintf(writer, "This Kubernetes cluster deployed without MutatingAdmissionWebhook support."+
			"See https://istio.io/docs/setup/kubernetes/additional-setup/sidecar-injection/#automatic-sidecar-injection\n")
	} else {
		fmt.Fprintf(writer, "Istio Configured to use automatic sidecar injection for 'default' namespace."+
			" To enable injection for other namespaces see https://istio.io/docs/setup/kubernetes/additional-setup/sidecar-injection/#deploying-an-app\n")
	}
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "-----------------------\n")
	fmt.Fprintf(writer, "Install Pre-Check passed! The cluster is ready for Istio installation.\n")
	fmt.Fprintf(writer, "\n")
	return nil

}

func checkKubernetesVersion(versionInfo *version.Info) bool {

	v := strings.Join([]string{versionInfo.Major, versionInfo.Minor}, ".")
	return parseVersion(minK8SVersion, 4) < parseVersion(v, 4)
}
func parseVersion(s string, width int) int64 {
	strList := strings.Split(s, ".")
	format := fmt.Sprintf("%%s%%0%ds", width)
	v := ""
	for _, value := range strList {
		v = fmt.Sprintf(format, v, value)
	}
	var result int64
	var err error
	if result, err = strconv.ParseInt(v, 10, 64); err != nil {
		return 0
	}
	return result
}

func checkCanCreateResources(c preCheckExecClient, namespace, group, version, name string) error {
	s := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     group,
				Version:   version,
				Resource:  name,
			},
		},
	}

	response, err := c.checkAuthorization(s)
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if len(response.Status.Reason) > 0 {
			msg := fmt.Sprintf("Istio installation will not succeed.Create permission lacking for:%s: %v", name, response.Status.Reason)
			return errors.New(msg)
		}
		msg := fmt.Sprintf("Istio installation will not succeed. Create permission lacking for:%s", name)
		return errors.New(msg)
	}
	return nil
}

func createKubeClient(restClientGetter resource.RESTClientGetter) (preCheckExecClient, error) {
	restConfig, err := restClientGetter.ToRESTConfig()

	if err != nil {
		return nil, err
	}
	k, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return &preCheckClient{client: k}, nil
}

func (c *preCheckClient) serverVersion() (*version.Info, error) {
	v, err := c.client.Discovery().ServerVersion()
	return v, err
}

func (c *preCheckClient) getNameSpace(ns string) (*v1.Namespace, error) {
	v, err := c.client.CoreV1().Namespaces().Get(ns, meta_v1.GetOptions{})
	return v, err
}

func (c *preCheckClient) checkAuthorization(s *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error) {
	response, err := c.client.AuthorizationV1beta1().SelfSubjectAccessReviews().Create(s)
	return response, err
}

func (c *preCheckClient) checkMutatingWebhook() error {
	_, err := c.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(meta_v1.ListOptions{})
	return err
}

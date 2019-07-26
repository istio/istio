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
	"regexp"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	authorizationapi "k8s.io/api/authorization/v1beta1"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/kubernetes"
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

func installPreCheck(istioNamespaceFlag string, restClientGetter resource.RESTClientGetter, writer io.Writer) error {
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Checking the cluster to make sure it is ready for Istio installation...\n")
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Kubernetes-api\n")
	fmt.Fprintf(writer, "-----------------------\n")
	var errs error
	c, err := clientExecFactory(restClientGetter)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to initialize the Kubernetes client: %v", err))
		fmt.Fprintf(writer, fmt.Sprintf("Failed to initialize the Kubernetes client: %v.\n", err))
	} else {
		fmt.Fprintf(writer, "Can initialize the Kubernetes client.\n")
	}
	v, err := c.serverVersion()
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to query the Kubernetes API Server: %v", err))
		fmt.Fprintf(writer, fmt.Sprintf("Failed to query the Kubernetes API Server: %v.\n", err))
	} else {
		fmt.Fprintf(writer, "Can query the Kubernetes API Server.\n")
	}
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Kubernetes-version\n")
	fmt.Fprintf(writer, "-----------------------\n")
	res, err := checkKubernetesVersion(v)
	if err != nil {
		errs = multierror.Append(errs, err)
		fmt.Fprint(writer, err)
	} else if !res {
		msg := fmt.Sprintf("The Kubernetes API version: %v is lower than the minimum version: "+minK8SVersion, v)
		errs = multierror.Append(errs, errors.New(msg))
		fmt.Fprintf(writer, msg+"\n")
	} else {
		fmt.Fprintf(writer, "Istio is compatible with Kubernetes: %v.\n", v)
	}

	fmt.Fprintf(writer, "\n\n")
	fmt.Fprintf(writer, "Istio-existence\n")
	fmt.Fprintf(writer, "-----------------------\n")
	_, err = c.getNameSpace(istioNamespaceFlag)
	if err == nil {
		msg := fmt.Sprintf("Istio cannot be installed because the Istio namespace '%v' is already in use", istioNamespaceFlag)
		errs = multierror.Append(errs, errors.New(msg))
		fmt.Fprintf(writer, msg+"\n")
	} else {
		fmt.Fprintf(writer, "Istio will be installed in the %v namespace.\n", istioNamespaceFlag)
	}

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
			namespace: istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "ClusterRole",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "ClusterRoleBinding",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "apiextensions.k8s.io",
			version:   "v1beta1",
			name:      "CustomResourceDefinition",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "rbac.authorization.k8s.io",
			version:   "v1beta1",
			name:      "Role",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "ServiceAccount",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "Service",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "extensions",
			version:   "v1beta1",
			name:      "Deployments",
		},
		{
			namespace: istioNamespaceFlag,
			group:     "",
			version:   "v1",
			name:      "ConfigMap",
		},
	}
	var createErrors error
	resourceNames := make([]string, 0)
	errResourceNames := make([]string, 0)
	for _, r := range Resources {
		err = checkCanCreateResources(c, r.namespace, r.group, r.version, r.name)
		if err != nil {
			createErrors = multierror.Append(createErrors, err)
			errs = multierror.Append(errs, err)
			errResourceNames = append(errResourceNames, r.name)
		}
		resourceNames = append(resourceNames, r.name)
	}
	if createErrors == nil {
		fmt.Fprintf(writer, "Can create necessary Kubernetes configurations: %v. \n", strings.Join(resourceNames, ","))
	} else {
		fmt.Fprintf(writer, "Can not create necessary Kubernetes configurations: %v. \n", strings.Join(errResourceNames, ","))
	}

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "SideCar-Injector\n")
	fmt.Fprintf(writer, "-----------------------\n")
	err = c.checkMutatingWebhook()
	if err != nil {
		fmt.Fprintf(writer, "This Kubernetes cluster deployed without MutatingAdmissionWebhook support."+
			"See https://istio.io/docs/setup/kubernetes/additional-setup/sidecar-injection/#automatic-sidecar-injection\n")
	} else {
		fmt.Fprintf(writer, "This Kubernetes cluster supports automatic sidecar injection."+
			" To enable automatic sidecar injection see https://istio.io/docs/setup/kubernetes/additional-setup/sidecar-injection/#deploying-an-app\n")
	}
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "-----------------------\n")
	if errs == nil {
		fmt.Fprintf(writer, "Install Pre-Check passed! The cluster is ready for Istio installation.\n")
	}
	fmt.Fprintf(writer, "\n")
	return errs

}

func checkKubernetesVersion(versionInfo *version.Info) (bool, error) {
	v, err := extractKubernetesVersion(versionInfo)
	if err != nil {
		return false, err
	}
	return parseVersion(minK8SVersion, 4) < parseVersion(v, 4), nil
}
func extractKubernetesVersion(versionInfo *version.Info) (string, error) {
	versionMatchRE := regexp.MustCompile(`^\s*v?([0-9]+(?:\.[0-9]+)*)(.*)*$`)
	parts := versionMatchRE.FindStringSubmatch(versionInfo.GitVersion)
	if parts == nil {
		return "", fmt.Errorf("could not parse %q as version", versionInfo.GitVersion)
	}
	numbers := parts[1]
	components := strings.Split(numbers, ".")
	if len(components) <= 1 {
		return "", fmt.Errorf("the version %q is invalid", versionInfo.GitVersion)
	}
	v := strings.Join([]string{components[0], components[1]}, ".")
	return v, nil
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

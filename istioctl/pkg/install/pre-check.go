// Copyright Istio Authors.
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
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/ghodss/yaml"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	authorizationapi "k8s.io/api/authorization/v1beta1"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	operator_v1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
)

var (
	clientFactory = createKubeClient
)

type istioInstall struct {
	namespace string
	revision  string
}

type preCheckClient struct {
	client  *kubernetes.Clientset
	dclient dynamic.Interface
}
type preCheckExecClient interface {
	getNameSpace(ns string) (*v1.Namespace, error)
	serverVersion() (*version.Info, error)
	checkAuthorization(s *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error)
	checkMutatingWebhook() error
	getIstioInstalls() ([]istioInstall, error)
}

// Tell the user if Istio can be installed, and if not give the reason.
// Note: this doesn't check the IstioOperator options.  It only checks a few things every
// Istio install needs.  It does not check the Revision.
func installPreCheck(istioNamespaceFlag string, restClientGetter genericclioptions.RESTClientGetter, writer io.Writer) error {
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Checking the cluster to make sure it is ready for Istio installation...\n")
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "#1. Kubernetes-api\n")
	fmt.Fprintf(writer, "-----------------------\n")
	var errs error
	c, err := clientFactory(restClientGetter)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to initialize the Kubernetes client: %v", err))
		fmt.Fprintf(writer, "Failed to initialize the Kubernetes client: %v.\n", err)
		return errs
	}
	fmt.Fprintf(writer, "Can initialize the Kubernetes client.\n")
	v, err := c.serverVersion()
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to query the Kubernetes API Server: %v", err))
		fmt.Fprintf(writer, "Failed to query the Kubernetes API Server: %v.\n", err)
		fmt.Fprintf(writer, "Istio install NOT verified because the cluster is unreachable.\n")
		return errs
	}
	fmt.Fprintf(writer, "Can query the Kubernetes API Server.\n")

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "#2. Kubernetes-version\n")
	fmt.Fprintf(writer, "-----------------------\n")
	res, err := k8sversion.CheckKubernetesVersion(v)
	if err != nil {
		errs = multierror.Append(errs, err)
		fmt.Fprint(writer, err)
	} else if !res {
		msg := fmt.Sprintf("The Kubernetes API version: %v is lower than the minimum version: "+k8sversion.MinK8SVersion, v)
		errs = multierror.Append(errs, errors.New(msg))
		fmt.Fprintf(writer, msg+"\n")
	} else {
		fmt.Fprintf(writer, "Istio is compatible with Kubernetes: %v.\n", v)
	}

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "#3. Istio-existence\n")
	fmt.Fprintf(writer, "-----------------------\n")
	_, _ = c.getNameSpace(istioNamespaceFlag)
	fmt.Fprintf(writer, "Istio will be installed in the %v namespace.\n", istioNamespaceFlag)

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "#4. Kubernetes-setup\n")
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
	fmt.Fprintf(writer, "#5. SideCar-Injector\n")
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

func createKubeClient(restClientGetter genericclioptions.RESTClientGetter) (preCheckExecClient, error) {
	restConfig, err := restClientGetter.ToRESTConfig()

	if err != nil {
		return nil, err
	}
	k, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	dk, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return &preCheckClient{client: k, dclient: dk}, nil
}

func (c *preCheckClient) serverVersion() (*version.Info, error) {
	v, err := c.client.Discovery().ServerVersion()
	return v, err
}

func (c *preCheckClient) getNameSpace(ns string) (*v1.Namespace, error) {
	v, err := c.client.CoreV1().Namespaces().Get(context.TODO(), ns, meta_v1.GetOptions{})
	return v, err
}

func (c *preCheckClient) checkAuthorization(s *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error) {
	response, err := c.client.AuthorizationV1beta1().SelfSubjectAccessReviews().Create(context.TODO(), s, meta_v1.CreateOptions{})
	return response, err
}

func (c *preCheckClient) checkMutatingWebhook() error {
	_, err := c.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(context.TODO(), meta_v1.ListOptions{})
	return err
}

func (c *preCheckClient) getIstioInstalls() ([]istioInstall, error) {
	return findIstios(c.dclient)
}

// NewPrecheckCommand creates a new command for checking a Kubernetes cluster for Istio compatibility
func NewPrecheckCommand() *cobra.Command {
	var (
		kubeConfigFlags = &genericclioptions.ConfigFlags{
			Context:    strPtr(""),
			Namespace:  strPtr(""),
			KubeConfig: strPtr(""),
		}

		filenames     = []string{}
		fileNameFlags = &genericclioptions.FileNameFlags{
			Filenames: &filenames,
			Recursive: boolPtr(false),
			Usage:     "Istio YAML installation file.",
		}
		istioNamespace string
		opts           clioptions.ControlPlaneOptions
	)
	precheckCmd := &cobra.Command{
		Use:   "precheck [-f <deployment or istio operator file>]",
		Short: "Checks Istio cluster compatibility",
		Long: `
		precheck inspects a Kubernetes cluster for Istio install requirements.
`,
		Example: `
		# Verify that Istio can be installed
		istioctl experimental precheck

		# Verify the deployment matches a custom Istio deployment configuration
		istioctl x precheck --set profile=demo

		# Verify the deployment matches the Istio Operator deployment definition
		istioctl x precheck -f iop.yaml
`,
		Args: cobra.ExactArgs(0),
		RunE: func(c *cobra.Command, args []string) error {
			targetNamespace := istioNamespace
			targetRevision := opts.Revision
			specific := c.Flags().Changed("istioNamespace") // is user asking about a specific Istio System ns or revision

			// Check if we can install the IOP specified with -f
			if len(fileNameFlags.ToOptions().Filenames) > 0 {
				iop, err := getIOPFromFile(fileNameFlags.ToOptions().Filenames[0])
				if err != nil {
					// Failure here means EITHER the file wasn't an IOP, or we can't parse
					// the IOP yet.
					return err
				}
				// Currently we don't look at specific IOP options, just the namespace and Revision
				targetNamespace = iop.GetNamespace()
				targetRevision = iop.Spec.Revision
				specific = true
			}

			cli, err := clientFactory(kubeConfigFlags)
			if err != nil {
				return err
			}

			installs, err := cli.getIstioInstalls()
			if err == nil && len(installs) > 0 {
				matched := false
				for _, install := range installs {
					if !specific || targetNamespace == install.namespace && targetRevision == install.revision {
						c.Printf("Istio Revision %q already installed in namespace %q\n", install.revision, install.namespace)
					}
					if targetNamespace == install.namespace && targetRevision == install.revision {
						matched = true
					}
				}
				// The user has Istio, but wants to install a new revision
				if !matched {
					return installPreCheck(targetNamespace, kubeConfigFlags, c.OutOrStdout())
				}
				return nil
			}

			// No IstioOperator was found.  In 1.6.0 we fall back to checking for Istio namespace
			nsExists, err := namespaceExists(targetNamespace, kubeConfigFlags)
			if err != nil {
				return err
			}
			if !nsExists {
				return installPreCheck(targetNamespace, kubeConfigFlags, c.OutOrStdout())
			}
			if specific {
				return installPreCheck(targetNamespace, kubeConfigFlags, c.OutOrStdout())
			}

			// The Istio namespace does exist, but it wasn't installed by 1.6.0+ because no
			// IstioOperator is there.
			c.Printf("Istio already installed in namespace %q.  Skipping pre-check.  Confirm with 'istioctl verify-install'.\n", targetNamespace)
			c.Printf("Use 'istioctl upgrade' to upgrade or 'istioctl install --set revision=<revision>' to install another control plane.\n")
			return nil
		},
	}

	flags := precheckCmd.PersistentFlags()
	flags.StringVarP(&istioNamespace, "istioNamespace", "i", controller.IstioNamespace,
		"Istio system namespace")
	kubeConfigFlags.AddFlags(flags)
	fileNameFlags.AddFlags(flags)
	opts.AttachControlPlaneFlags(precheckCmd)
	return precheckCmd
}

func findIstios(client dynamic.Interface) ([]istioInstall, error) {
	retval := make([]istioInstall, 0)

	// First, look for IstioOperator CRs left by 'istioctl install' or 'kubectl apply'
	iops, err := allOperatorsInCluster(client)
	if err != nil {
		return retval, err
	}
	for _, iop := range iops {
		retval = append(retval, istioInstall{
			namespace: iop.Namespace,
			revision:  iop.Spec.Revision,
		})
	}
	return retval, nil
}

func getIOPFromFile(filename string) (*operator_v1alpha1.IstioOperator, error) {
	// (DON'T use genericclioptions to read IstioOperator.  It depends on the cluster
	// having a CRD for istiooperators, and at precheck time that might not exist.)
	iopYaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	content := make(map[string]interface{})
	err = yaml.Unmarshal(iopYaml, &content)
	if err != nil {
		return nil, err
	}
	un := &unstructured.Unstructured{Object: content}
	if un.GetKind() != "IstioOperator" {
		return nil, fmt.Errorf("%q must contain an IstioOperator", filename)
	}

	// IstioOperator isn't part of pkg/config/schema/collections,
	// usual conversion not available.  Convert unstructured to string
	// and ask operator code to unmarshal.

	un.SetCreationTimestamp(meta_v1.Time{}) // UnmarshalIstioOperator chokes on these
	by := util.ToYAML(un)
	iop, err := operator_istio.UnmarshalIstioOperator(by, true)
	if err != nil {
		return nil, err
	}
	return iop, nil
}

func namespaceExists(ns string, restClientGetter genericclioptions.RESTClientGetter) (bool, error) {
	c, err := clientFactory(restClientGetter)
	if err != nil {
		return false, err
	}
	_, err = c.getNameSpace(ns)
	return err == nil, nil
}

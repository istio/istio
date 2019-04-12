// Copyright 2018 Istio Authors.
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
	"strings"

	"github.com/spf13/cobra"
	v1batch "k8s.io/api/batch/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	scheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"

	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
)

var (
	verifyInstallCmd *cobra.Command
)

func verifyInstall(enableVerbose bool, istioNamespaceFlag *string,
	restClientGetter resource.RESTClientGetter, options resource.FilenameOptions,
	writer io.Writer, args []string) error {
	if len(options.Filenames) == 0 {
		if len(args) != 0 {
			fmt.Fprint(writer, verifyInstallCmd.UsageString())
			return fmt.Errorf("verify-install takes no arguments to perform installation pre-check")
		}
		return installPreCheck(istioNamespaceFlag, restClientGetter, writer)
	}
	return verifyPostInstall(enableVerbose, istioNamespaceFlag, restClientGetter,
		options, writer)

}

func verifyPostInstall(enableVerbose bool, istioNamespaceFlag *string,
	restClientGetter resource.RESTClientGetter, options resource.FilenameOptions, writer io.Writer) error {
	crdCount := 0
	istioDeploymentCount := 0
	r := resource.NewBuilder(restClientGetter).
		Unstructured().
		FilenameParam(false, &options).
		Flatten().
		Do()
	if err := r.Err(); err != nil {
		return err
	}
	err := r.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			return err
		}
		un := &unstructured.Unstructured{Object: content}
		kind := un.GetKind()
		name := un.GetName()
		namespace := un.GetNamespace()
		kinds := findResourceInSpec(kind)
		if kinds == "" {
			kinds = strings.ToLower(kind) + "s"
		}
		if namespace == "" {
			namespace = "default"
		}
		switch kind {
		case "Deployment":
			deployment := &v1beta1.Deployment{}
			err = info.Client.
				Get().
				Resource(kinds).
				Namespace(namespace).
				Name(name).
				VersionedParams(&meta_v1.GetOptions{}, scheme.ParameterCodec).
				Do().
				Into(deployment)
			if err != nil {
				return err
			}
			err = getDeploymentStatus(deployment, name, options.Filenames[0])
			if err != nil {
				return err
			}
			if namespace == *istioNamespaceFlag && strings.HasPrefix(name, "istio-") {
				istioDeploymentCount++
			}
		case "Job":
			job := &v1batch.Job{}
			err = info.Client.
				Get().
				Resource(kinds).
				Namespace(namespace).
				Name(name).
				VersionedParams(&meta_v1.GetOptions{}, scheme.ParameterCodec).
				Do().
				Into(job)
			if err != nil {
				return err
			}
			for _, c := range job.Status.Conditions {
				if c.Type == v1batch.JobFailed {
					msg := fmt.Sprintf("Istio installation failed, incomplete or"+
						" does not match \"%s\" - the required Job  %s failed", options.Filenames[0], name)
					return errors.New(msg)
				}
			}
		default:
			result := info.Client.
				Get().
				Resource(kinds).
				Name(name).
				Do()
			if result.Error() != nil {
				result = info.Client.
					Get().
					Resource(kinds).
					Namespace(namespace).
					Name(name).
					Do()
				if result.Error() != nil {
					msg := fmt.Sprintf("Istio installation failed, incomplete or"+
						" does not match \"%s\" - the required %s:%s is not ready due to: %v", options.Filenames[0], kind, name, result.Error())
					return errors.New(msg)
				}
			}
			if kind == "CustomResourceDefinition" {
				crdCount++
			}
		}
		if enableVerbose {
			fmt.Fprintf(writer, "%s: %s.%s checked successfully\n", kind, name, namespace)
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(writer, "Checked %v crds\n", crdCount)
	fmt.Fprintf(writer, "Checked %v Istio Deployments\n", istioDeploymentCount)
	fmt.Fprintf(writer, "Istio is installed successfully\n")
	return nil
}

// NewVerifyCommand creates a new command for verifying Istio Installation Status
func NewVerifyCommand(istioNamespaceFlag *string) *cobra.Command {
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
		enableVerbose bool
	)
	verifyInstallCmd = &cobra.Command{
		Use:   "verify-install",
		Short: "Verifies Istio Installation Status or performs pre-check for the cluster before Istio installation",
		Long: `
		verify-install verifies Istio installation status against the installation file
		you specified when you installed Istio. It loops through all the installation
		resources defined in your installation file and reports whether all of them are
		in ready status. It will report failure when any of them are not ready.

		If you do not specify installation file it will perform pre-check for your cluster
		and report whether the cluster is ready for Istio installation.
`,
		Example: `
		# Verify that Istio can be freshly installed
		istioctl experimental verify-install
		
		# Verify that the deployment matches the istio-demo profile
		istioctl experimental verify-install -f istio-demo.yaml
		
		# Verify the deployment matches a custom Istio deployment configuration
		istioctl experimental verify-install -f $HOME/istio.yaml
`,
		RunE: func(c *cobra.Command, args []string) error {
			return verifyInstall(enableVerbose, istioNamespaceFlag, kubeConfigFlags,
				fileNameFlags.ToOptions(), c.OutOrStderr(), args)
		},
	}

	flags := verifyInstallCmd.PersistentFlags()
	kubeConfigFlags.AddFlags(flags)
	fileNameFlags.AddFlags(flags)
	verifyInstallCmd.Flags().BoolVar(&enableVerbose, "enableVerbose", true,
		"Enable verbose output")
	return verifyInstallCmd
}

func strPtr(val string) *string {
	return &val
}

func boolPtr(val bool) *bool {
	return &val
}

func getDeploymentStatus(deployment *v1beta1.Deployment, name, fileName string) error {
	cond := getDeploymentCondition(deployment.Status, v1beta1.DeploymentProgressing)
	if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match \"%s\""+
			" - deployment %q exceeded its progress deadline", fileName, name)
		return errors.New(msg)
	}
	if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match \"%s\""+
			" - waiting for deployment %q rollout to finish: %d out of %d new replicas have been updated",
			fileName, name, deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas)
		return errors.New(msg)
	}
	if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match \"%s\""+
			" - waiting for deployment %q rollout to finish: %d old replicas are pending termination",
			fileName, name, deployment.Status.Replicas-deployment.Status.UpdatedReplicas)
		return errors.New(msg)
	}
	if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match \"%s\""+
			" - waiting for deployment %q rollout to finish: %d of %d updated replicas are available",
			fileName, name, deployment.Status.AvailableReplicas, deployment.Status.UpdatedReplicas)
		return errors.New(msg)
	}
	return nil
}

func getDeploymentCondition(status v1beta1.DeploymentStatus, condType v1beta1.DeploymentConditionType) *v1beta1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func findResourceInSpec(kind string) string {
	for _, spec := range kube_meta.Types.All() {
		if spec.Kind == kind {
			return spec.Plural
		}
	}
	return ""
}

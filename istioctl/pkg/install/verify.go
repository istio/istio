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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	v1batch "k8s.io/api/batch/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"

	operator_istio "istio.io/istio/operator/pkg/apis/istio"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/version"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema"
)

var (
	verifyInstallCmd *cobra.Command
)

func verifyInstall(enableVerbose bool, istioNamespaceFlag string,
	restClientGetter genericclioptions.RESTClientGetter, options resource.FilenameOptions,
	writer io.Writer, args []string) error {
	if len(options.Filenames) == 0 {
		if len(args) != 0 {
			_, _ = fmt.Fprint(writer, verifyInstallCmd.UsageString())
			return fmt.Errorf("verify-install takes no arguments to perform installation pre-check")
		}
		return installPreCheck(istioNamespaceFlag, restClientGetter, writer)
	}

	// This is not a pre-check.  Check that the supplied resources exist in the cluster
	r := resource.NewBuilder(restClientGetter).
		Unstructured().
		FilenameParam(false, &options).
		Flatten().
		Do()
	if r.Err() != nil {
		return r.Err()
	}
	visitor := genericclioptions.ResourceFinderForResult(r).Do()
	return verifyPostInstallAndShow(enableVerbose, istioNamespaceFlag, visitor, options.Filenames[0], restClientGetter, writer)

}

func verifyPostInstallAndShow(enableVerbose bool, istioNamespaceFlag string,
	visitor resource.Visitor, filename string, restClientGetter genericclioptions.RESTClientGetter, writer io.Writer) error {
	crdCount, istioDeploymentCount, err := verifyPostInstall(enableVerbose,
		istioNamespaceFlag,
		visitor,
		filename,
		restClientGetter,
		writer)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "Checked %v custom resource definitions\n", crdCount)
	_, _ = fmt.Fprintf(writer, "Checked %v Istio Deployments\n", istioDeploymentCount)
	if istioDeploymentCount == 0 {
		_, _ = fmt.Fprintf(writer, "No Istio installation found\n")
		return fmt.Errorf("no Istio installation found")
	}
	_, _ = fmt.Fprintf(writer, "Istio is installed successfully\n")
	return nil
}

func verifyPostInstall(enableVerbose bool, istioNamespaceFlag string,
	visitor resource.Visitor, filename string, restClientGetter genericclioptions.RESTClientGetter, writer io.Writer) (int, int, error) {
	crdCount := 0
	istioDeploymentCount := 0
	err := visitor.Visit(func(info *resource.Info, err error) error {
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
			deployment := &appsv1.Deployment{}
			err = info.Client.
				Get().
				Resource(kinds).
				Namespace(namespace).
				Name(name).
				VersionedParams(&meta_v1.GetOptions{}, scheme.ParameterCodec).
				Do(context.TODO()).
				Into(deployment)
			if err != nil {
				return err
			}
			err = getDeploymentStatus(deployment, name, filename)
			if err != nil {
				return err
			}
			if namespace == istioNamespaceFlag && strings.HasPrefix(name, "istio-") {
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
				Do(context.TODO()).
				Into(job)
			if err != nil {
				return err
			}
			for _, c := range job.Status.Conditions {
				if c.Type == v1batch.JobFailed {
					msg := fmt.Sprintf("Istio installation failed, incomplete or"+
						" does not match \"%s\" - the required Job %s failed", filename, name)
					return errors.New(msg)
				}
			}
		case "IstioOperator":
			result := info.Client.
				Get().
				Resource(kinds).
				Namespace(namespace).
				Name(name).
				Do(context.TODO())
			if result.Error() != nil {
				return err
			}
			obj, _ := result.Get()
			un, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("could not read IstioOperator")
			}

			// IstioOperator isn't part of pkg/config/schema/collections,
			// usual conversion not available.  Convert unstructured to string
			// and ask operator code to check.

			un.SetCreationTimestamp(meta_v1.Time{}) // UnmarshalIstioOperator chokes on these
			by, err := json.Marshal(un)
			if err != nil {
				return err
			}

			iop, err := operator_istio.UnmarshalIstioOperator(string(by))
			if err != nil {
				return err
			}
			generatedCrds, generatedDeployments, err := verifyPostInstallIstioOperator(enableVerbose, istioNamespaceFlag, iop, filename, restClientGetter, writer)
			if err != nil {
				return err
			}
			crdCount += generatedCrds
			istioDeploymentCount += generatedDeployments
		default:
			result := info.Client.
				Get().
				Resource(kinds).
				Name(name).
				Do(context.TODO())
			if result.Error() != nil {
				result = info.Client.
					Get().
					Resource(kinds).
					Namespace(namespace).
					Name(name).
					Do(context.TODO())
				if result.Error() != nil {
					msg := fmt.Sprintf("Istio installation failed, incomplete or"+
						" does not match \"%s\" - the required %s:%s is not ready due to: %v", filename, kind, name, result.Error())
					return errors.New(msg)
				}
			}
			if kind == "CustomResourceDefinition" {
				crdCount++
			}
		}
		if enableVerbose {
			_, _ = fmt.Fprintf(writer, "%s: %s.%s checked successfully\n", kind, name, namespace)
		}
		return nil
	})
	if err != nil {
		return crdCount, istioDeploymentCount, err
	}
	return crdCount, istioDeploymentCount, nil
}

// NewVerifyCommand creates a new command for verifying Istio Installation Status
func NewVerifyCommand() *cobra.Command {
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
		enableVerbose  bool
		istioNamespace string
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
		istioctl verify-install
		
		# Verify the deployment matches a custom Istio deployment configuration
		istioctl verify-install -f $HOME/istio.yaml
`,
		RunE: func(c *cobra.Command, args []string) error {
			return verifyInstall(enableVerbose, istioNamespace, kubeConfigFlags,
				fileNameFlags.ToOptions(), c.OutOrStderr(), args)
		},
	}

	flags := verifyInstallCmd.PersistentFlags()
	flags.StringVarP(&istioNamespace, "istioNamespace", "i", controller.IstioNamespace,
		"Istio system namespace")
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

func getDeploymentStatus(deployment *appsv1.Deployment, name, fileName string) error {
	cond := getDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)
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

func getDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func findResourceInSpec(kind string) string {
	for _, c := range schema.MustGet().KubeCollections().All() {
		if c.Resource().Kind() == kind {
			return c.Resource().Plural()
		}
	}
	return ""
}

// nolint: lll
func verifyPostInstallIstioOperator(enableVerbose bool, istioNamespaceFlag string, iop *v1alpha1.IstioOperator, filename string, restClientGetter genericclioptions.RESTClientGetter, writer io.Writer) (int, int, error) {
	// Generate the manifest this IstioOperator will make
	t, err := translate.NewTranslator(version.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return 0, 0, err
	}

	cp, err := controlplane.NewIstioOperator(iop.Spec, t)
	if err != nil {
		return 0, 0, err
	}
	if err := cp.Run(); err != nil {
		return 0, 0, err
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		return 0, 0, errs.ToError()
	}

	builder := resource.NewBuilder(restClientGetter).Unstructured()
	for cat, manifest := range manifests {
		for i, manitem := range manifest {
			reader := strings.NewReader(manitem)
			pseudoFilename := fmt.Sprintf("%s:%d generated from %s", cat, i, filename)
			builder = builder.Stream(reader, pseudoFilename)
		}
	}
	r := builder.Flatten().Do()
	if r.Err() != nil {
		return 0, 0, r.Err()
	}
	visitor := genericclioptions.ResourceFinderForResult(r).Do()
	// Indirectly RECURSE back into verifyPostInstall with the manifest we just generated
	generatedCrds, generatedDeployments, err := verifyPostInstall(enableVerbose,
		istioNamespaceFlag,
		visitor,
		fmt.Sprintf("generated from %s", filename),
		restClientGetter,
		writer)
	if err != nil {
		return generatedCrds, generatedDeployments, err
	}

	return generatedCrds, generatedDeployments, nil
}

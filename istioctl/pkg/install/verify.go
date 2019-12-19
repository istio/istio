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

	"istio.io/pkg/log"

	"istio.io/operator/cmd/mesh"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/object"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	v1batch "k8s.io/api/batch/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/istio/galley/pkg/config/meta/metadata"
)

var (
	verifyInstallCmd *cobra.Command
)

type verifyInstallArgs struct {
	kubeConfigFlags *genericclioptions.ConfigFlags
	fileNameFlags   *genericclioptions.FileNameFlags
	istioNamespace  string
	set             []string
	enableVerbose   bool
	preCheck        bool
}

// NewVerifyCommand creates a new command for verifying Istio Installation Status
func NewVerifyCommand() *cobra.Command {
	vArgs := &verifyInstallArgs{}
	verifyInstallCmd = &cobra.Command{
		Use:   "verify-install",
		Short: "Verifies Istio Installation Status or performs pre-check for the cluster before Istio installation",
		Long: `
		verify-install verifies Istio installation status against the installation file or the IstioControlPlane 
		CustomResource you specified when you installed Istio. It loops through all the installation
		resources defined in your installation file and reports whether all of them are
		in ready status. It will report failure when any of them are not ready.

		If you do not specify installation file or specify --pre-check it will perform pre-check for your cluster
		and report whether the cluster is ready for Istio installation.
`,
		Example: `
		# Verify that Istio can be freshly installed
		istioctl verify-install

		# Verify that Istio can be freshly installed into 'istio-system' namespace
		istioctl verify-install --pre-check -s defaultNamespace=istio-system
		
		# Verify the deployment matches a custom Istio deployment configuration
		istioctl verify-install -f $HOME/istio.yaml

		# Verify the deployment matches an IstioControlPlane CustomResource
		istioctl verify-install -f $HOME/istio_v1alpha2_istiocontrolplane_cr.yaml

		# Verify the deployment matches minimal profile for IstioControlPlane CustomResource
		istioctl verify-install -s profile=minimal
`,
		RunE: func(c *cobra.Command, args []string) error {
			l := mesh.NewLogger(false, c.OutOrStderr(), c.OutOrStderr())
			return verifyInstall(vArgs, c.OutOrStderr(), args, l)
		},
	}

	addVerifyInstallFlags(verifyInstallCmd, vArgs)
	return verifyInstallCmd
}

func addVerifyInstallFlags(cmd *cobra.Command, v *verifyInstallArgs) {
	var (
		filenames     = []string{}
		fileNameFlags = &genericclioptions.FileNameFlags{
			Filenames: &filenames,
			Recursive: boolPtr(false),
			Usage:     "Istio YAML installation file or IstioControlPlane CustomResource file",
		}
		kubeConfigFlags = &genericclioptions.ConfigFlags{
			Context:    strPtr(""),
			Namespace:  strPtr(""),
			KubeConfig: strPtr(""),
		}
	)
	flags := cmd.PersistentFlags()
	flags.StringVarP(&v.istioNamespace, "istioNamespace", "i", "istio-system",
		"Istio system namespace")
	verifyInstallCmd.PersistentFlags().StringSliceVarP(&v.set, "set", "s", nil, mesh.SetFlagHelpStr)
	verifyInstallCmd.PersistentFlags().BoolVar(&v.enableVerbose, "enableVerbose", true,
		"Enable verbose output")
	verifyInstallCmd.PersistentFlags().BoolVar(&v.preCheck, "pre-check", false,
		"perform pre-check for your cluster ")
	kubeConfigFlags.AddFlags(flags)
	v.kubeConfigFlags = kubeConfigFlags
	fileNameFlags.AddFlags(flags)
	v.fileNameFlags = fileNameFlags
}

func verifyInstall(v *verifyInstallArgs, writer io.Writer, args []string, l *mesh.Logger) error {
	options := v.fileNameFlags.ToOptions()
	noargs := len(options.Filenames) == 0 && v.set == nil && len(args) == 0
	fileName := ""
	compareFile := strings.Join(v.set, ",")
	var manifest name.ManifestMap
	if len(options.Filenames) != 0 || v.set != nil {
		overlayFromSet, err := mesh.MakeTreeFromSetList(v.set, true, l)
		if err != nil {
			return err
		}
		options := v.fileNameFlags.ToOptions()
		if len(options.Filenames) != 0 {
			fileName = options.Filenames[0]
			compareFile = fileName
		}
		manifest, _, _ = mesh.GenManifests(fileName, overlayFromSet, true, l)
	}
	if v.preCheck || noargs {
		nsList := []string{}
		if manifest != nil {
			base := manifest[name.IstioBaseComponentName]
			objects, err := object.ParseK8sObjectsFromYAMLManifest(base)
			if err != nil {
				log.Debugf("fails to parse base yaml: %v", err)
			}
			for _, v := range objects {
				if v.Kind == "Namespace" {
					nsList = append(nsList, v.Name)
				}
			}
		}
		return installPreCheck(v, nsList, writer)
	}
	return verifyPostInstall(v, compareFile, manifest, writer)

}

func verifyPostInstall(v *verifyInstallArgs, c string, m name.ManifestMap, writer io.Writer) error {
	var crdCount, istioDeploymentCount int
	var r *resource.Result
	options := v.fileNameFlags.ToOptions()
	manifestStr := ""
	if m != nil {
		for _, v := range m {
			manifestStr += v
		}
		r = resource.NewBuilder(v.kubeConfigFlags).
			Unstructured().
			Stream(strings.NewReader(manifestStr), "manifest").
			Flatten().
			Do()
		if err := r.Err(); err != nil {
			return err
		}
	} else {
		r = resource.NewBuilder(v.kubeConfigFlags).
			Unstructured().
			FilenameParam(false, &options).
			Flatten().
			Do()
		if err := r.Err(); err != nil {
			return err
		}
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
			deployment := &appsv1.Deployment{}
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
			if namespace == v.istioNamespace && strings.HasPrefix(name, "istio-") {
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
						" does not match %q - the required Job  %s failed", c, name)
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
						" does not match %q - the required %s:%s is not ready due to: %v", c, kind, name, result.Error())
					return errors.New(msg)
				}
			}
			if kind == "CustomResourceDefinition" {
				crdCount++
			}
		}
		if v.enableVerbose {
			_, _ = fmt.Fprintf(writer, "%s: %s.%s checked successfully\n", kind, name, namespace)
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "Checked %v crds\n", crdCount)
	_, _ = fmt.Fprintf(writer, "Checked %v Istio Deployments\n", istioDeploymentCount)
	_, _ = fmt.Fprintf(writer, "Istio is installed successfully\n")
	return nil
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
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match %q"+
			" - deployment %q exceeded its progress deadline", fileName, name)
		return errors.New(msg)
	}
	if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match %q"+
			" - waiting for deployment %q rollout to finish: %d out of %d new replicas have been updated",
			fileName, name, deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas)
		return errors.New(msg)
	}
	if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match %q"+
			" - waiting for deployment %q rollout to finish: %d old replicas are pending termination",
			fileName, name, deployment.Status.Replicas-deployment.Status.UpdatedReplicas)
		return errors.New(msg)
	}
	if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		msg := fmt.Sprintf("Istio installation failed, incomplete or does not match %q"+
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
	for _, r := range metadata.MustGet().KubeSource().Resources() {
		if r.Kind == kind {
			return r.Plural
		}
	}
	return ""
}

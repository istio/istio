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

package cmd

import (
	"fmt"
	"io"
	"io/ioutil"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/istioctl/pkg/util/handlers"
	istiocmd "istio.io/istio/pilot/cmd"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/log"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func addToMeshCmd() *cobra.Command {
	addToMeshCmd := &cobra.Command{
		Use:     "add-to-mesh",
		Aliases: []string{"add"},
		Short:   "Add workloads into Istio service mesh",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown resource type %q", args[0])
			}
			return nil
		},
	}
	addToMeshCmd.AddCommand(svcMeshifyCmd())
	addToMeshCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfigFile", "",
		"mesh configuration filename. Takes precedence over --meshConfigMapName if set")
	addToMeshCmd.PersistentFlags().StringVar(&injectConfigFile, "injectConfigFile", "",
		"injection configuration filename. Cannot be used with --injectConfigMapName")
	addToMeshCmd.PersistentFlags().StringVar(&valuesFile, "valuesFile", "",
		"injection values configuration filename.")

	addToMeshCmd.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", defaultMeshConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
	addToMeshCmd.PersistentFlags().StringVar(&injectConfigMapName, "injectConfigMapName", defaultInjectConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio sidecar injection, key should be %q.", injectConfigMapKey))

	return addToMeshCmd
}

func svcMeshifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Add Service to Istio service mesh",
		Long: `istioctl experimental add-to-mesh restarts pods with the Istio sidecar.  Use 'add-to-mesh'
to test deployments for compatibility with Istio.  If your service does not function after
using 'add-to-mesh' you must re-deploy it and troubleshoot it for Istio compatibility.
See https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental add-to-mesh service productpage`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting service name")
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			var sidecarTemplate, valuesConfig string
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			writer := cmd.OutOrStdout()

			meshConfig, err := setupParameters(&sidecarTemplate, &valuesConfig)
			if err != nil {
				return err
			}
			matchingDeployments, err := findDeploymentsForSvc(client, ns, args[0])
			if err != nil {
				return err
			}
			if len(matchingDeployments) == 0 {
				fmt.Fprintf(writer, "No deployments found for service %s.%s\n", args[0], ns)
				return nil
			}
			return injectSideCarIntoDeployment(client, matchingDeployments, sidecarTemplate, valuesConfig,
				args[0], ns, meshConfig, writer)
		},
	}
	return cmd
}
func setupParameters(sidecarTemplate, valuesConfig *string) (*meshconfig.MeshConfig, error) {
	var meshConfig *meshconfig.MeshConfig
	var err error
	if meshConfigFile != "" {
		if meshConfig, err = istiocmd.ReadMeshConfig(meshConfigFile); err != nil {
			return nil, err
		}
	} else {
		if meshConfig, err = getMeshConfigFromConfigMap(kubeconfig); err != nil {
			return nil, err
		}
	}
	if injectConfigFile != "" {
		injectionConfig, err := ioutil.ReadFile(injectConfigFile) // nolint: vetshadow
		if err != nil {
			return nil, err
		}
		var injectConfig inject.Config
		if err := yaml.Unmarshal(injectionConfig, &injectConfig); err != nil {
			return nil, multierr.Append(fmt.Errorf("loading --injectConfigFile"), err)
		}
		*sidecarTemplate = injectConfig.Template
	} else if *sidecarTemplate, err = getInjectConfigFromConfigMap(kubeconfig); err != nil {
		return nil, err
	}
	if valuesFile != "" {
		valuesConfigBytes, err := ioutil.ReadFile(valuesFile) // nolint: vetshadow
		if err != nil {
			return nil, err
		}
		*valuesConfig = string(valuesConfigBytes)
	} else if *valuesConfig, err = getValuesFromConfigMap(kubeconfig); err != nil {
		return nil, err
	}
	return meshConfig, err
}

func injectSideCarIntoDeployment(client kubernetes.Interface, deps []appsv1.Deployment, sidecarTemplate, valuesConfig,
	svcName, svcNamespace string, meshConfig *meshconfig.MeshConfig, writer io.Writer) error {
	var errs error
	for _, dep := range deps {
		log.Debugf("updating deployment %s.%s with Istio sidecar injected",
			dep.Name, dep.Namespace)
		newDep, err := inject.IntoObject(sidecarTemplate, valuesConfig, meshConfig, &dep)
		if err != nil {
			errs = multierr.Append(fmt.Errorf("failed to update deployment %s.%s for service %s.%s due to %v",
				dep.Name, dep.Namespace, svcName, svcNamespace, err), errs)
			continue
		}
		res, b := newDep.(*appsv1.Deployment)
		if !b {
			errs = multierr.Append(fmt.Errorf("failed to update deployment %s.%s for service %s.%s",
				dep.Name, dep.Namespace, svcName, svcNamespace), errs)
			continue
		}
		if _, err :=
			client.AppsV1().Deployments(svcNamespace).Update(res); err != nil {
			errs = multierr.Append(fmt.Errorf("failed to update deployment %s.%s for service %s.%s due to %v",
				dep.Name, dep.Namespace, svcName, svcNamespace, err), errs)
			continue

		}
		if _, err = client.AppsV1().Deployments(svcNamespace).UpdateStatus(res); err != nil {
			errs = multierr.Append(fmt.Errorf("failed to update deployment %s.%s for service %s.%s due to %v",
				dep.Name, dep.Namespace, svcName, svcNamespace, err), errs)
			continue
		}
		fmt.Fprintf(writer, "deployment %s.%s updated successfully with Istio sidecar injected.\n"+
			"Next Step: Add related labels to the deployment to align with Istio's requirement: "+
			"https://istio.io/docs/setup/kubernetes/additional-setup/requirements/\n",
			dep.Name, dep.Namespace)
	}
	return errs
}

func findDeploymentsForSvc(client kubernetes.Interface, ns, name string) ([]appsv1.Deployment, error) {
	deps := []appsv1.Deployment{}
	svc, err := client.CoreV1().Services(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	svcSelector := k8s_labels.SelectorFromSet(svc.Spec.Selector)
	deployments, err := client.AppsV1().Deployments(ns).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments.Items {
		depLabels := k8s_labels.Set(dep.ObjectMeta.Labels)
		if svcSelector.Matches(depLabels) {
			deps = append(deps, dep)
		}
	}
	return deps, nil
}

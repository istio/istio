// Copyright 2017 Istio Authors
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

package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

const (
	configMapKey       = "mesh"
	injectConfigMapKey = "config"
	valuesConfigMapKey = "values"
)

func createInterface(kubeconfig string) (kubernetes.Interface, error) {
	restConfig, err := kube.BuildClientConfig(kubeconfig, configContext)

	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

func getMeshConfigFromConfigMap(kubeconfig string) (*meshconfig.MeshConfig, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return nil, err
	}

	config, err := client.CoreV1().ConfigMaps(istioNamespace).Get(meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not read valid configmap %q from namespace  %q: %v - "+
			"Use --meshConfigFile or re-run kube-inject with `-i <istioSystemNamespace> and ensure valid MeshConfig exists",
			meshConfigMapName, istioNamespace, err)
	}
	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	configYaml, exists := config.Data[configMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", configMapKey)
	}
	cfg, err := model.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		err = multierr.Append(fmt.Errorf("istioctl version %s cannot parse mesh config.  Install istioctl from the latest Istio release",
			version.Info.Version), err)
	}
	return cfg, err
}

// grabs the raw values from the ConfigMap. These are encoded as JSON.
func getValuesFromConfigMap(kubeconfig string) (string, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return "", err
	}

	config, err := client.CoreV1().ConfigMaps(istioNamespace).Get(injectConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not find valid configmap %q from namespace  %q: %v - "+
			"Use --injectConfigFile or re-run kube-inject with `-i <istioSystemNamespace> and ensure istio-inject configmap exists",
			injectConfigMapName, istioNamespace, err)
	}

	valuesData, exists := config.Data[valuesConfigMapKey]
	if !exists {
		return "", fmt.Errorf("missing configuration map key %q in %q",
			valuesConfigMapKey, injectConfigMapName)
	}

	return valuesData, nil
}

func getInjectConfigFromConfigMap(kubeconfig string) (string, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return "", err
	}

	config, err := client.CoreV1().ConfigMaps(istioNamespace).Get(injectConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not find valid configmap %q from namespace  %q: %v - "+
			"Use --injectConfigFile or re-run kube-inject with `-i <istioSystemNamespace> and ensure istio-inject configmap exists",
			injectConfigMapName, istioNamespace, err)
	}
	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	injectData, exists := config.Data[injectConfigMapKey]
	if !exists {
		return "", fmt.Errorf("missing configuration map key %q in %q",
			injectConfigMapKey, injectConfigMapName)
	}
	var injectConfig inject.Config
	if err := yaml.Unmarshal([]byte(injectData), &injectConfig); err != nil {
		return "", fmt.Errorf("unable to convert data from configmap %q: %v",
			injectConfigMapName, err)
	}
	log.Debugf("using inject template from configmap %q", injectConfigMapName)
	return injectConfig.Template, nil
}

func validateFlags() error {
	var err error
	if inFilename != "" && emitTemplate {
		err = multierr.Append(err, errors.New("--filename and --emitTemplate are mutually exclusive"))
	}
	if inFilename == "" && !emitTemplate {
		err = multierr.Append(err, errors.New("filename not specified (see --filename or -f)"))
	}
	if meshConfigFile == "" && meshConfigMapName == "" {
		err = multierr.Append(err, errors.New("--meshConfigFile or --meshConfigMapName must be set"))
	}
	return err
}

var (
	emitTemplate bool

	inFilename          string
	outFilename         string
	meshConfigFile      string
	meshConfigMapName   string
	valuesFile          string
	injectConfigFile    string
	injectConfigMapName string
)

var (
	injectCmd = &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject Envoy sidecar into Kubernetes pod resources",
		Long: `

kube-inject manually injects the Envoy sidecar into Kubernetes
workloads. Unsupported resources are left unmodified so it is safe to
run kube-inject over a single file that contains multiple Service,
ConfigMap, Deployment, etc. definitions for a complex application. Its
best to do this when the resource is initially created.

k8s.io/docs/concepts/workloads/pods/pod-overview/#pod-templates is
updated for Job, DaemonSet, ReplicaSet, Pod and Deployment YAML resource
documents. Support for additional pod-based resource types can be
added as necessary.

The Istio project is continually evolving so the Istio sidecar
configuration may change unannounced. When in doubt re-run istioctl
kube-inject on deployments to get the most up-to-date changes.

To override the sidecar injection template from kubernetes configmap
'istio-inject', the parameters --injectConfigFile or --injectConfigMapName
can be used. Either of options would typically be used with the
file/configmap created with a new Istio release.
`,
		Example: `
# Update resources on the fly before applying.
kubectl apply -f <(istioctl kube-inject -f <resource.yaml>)

# Create a persistent version of the deployment with Envoy sidecar
# injected.
istioctl kube-inject -f deployment.yaml -o deployment-injected.yaml

# Update an existing deployment.
kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -

# Create a persistent version of the deployment with Envoy sidecar
# injected configuration from Kubernetes configmap 'istio-inject'
istioctl kube-inject -f deployment.yaml -o deployment-injected.yaml --injectConfigMapName istio-inject
`,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			if err = validateFlags(); err != nil {
				return err
			}

			var reader io.Reader
			if !emitTemplate {
				if inFilename == "-" {
					reader = os.Stdin
				} else {
					var in *os.File
					if in, err = os.Open(inFilename); err != nil {
						return err
					}
					reader = in
					defer func() {
						if errClose := in.Close(); errClose != nil {
							log.Errorf("Error: close file from %s, %s", inFilename, errClose)

							// don't overwrite the previous error
							if err == nil {
								err = errClose
							}
						}
					}()
				}
			}

			var writer io.Writer
			if outFilename == "" {
				writer = c.OutOrStdout()
			} else {
				var out *os.File
				if out, err = os.Create(outFilename); err != nil {
					return err
				}
				writer = out
				defer func() {
					if errClose := out.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", outFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			var meshConfig *meshconfig.MeshConfig
			if meshConfigFile != "" {
				if meshConfig, err = cmd.ReadMeshConfig(meshConfigFile); err != nil {
					return err
				}
			} else {
				if meshConfig, err = getMeshConfigFromConfigMap(kubeconfig); err != nil {
					return err
				}
			}

			var sidecarTemplate string
			if injectConfigFile != "" {
				injectionConfig, err := ioutil.ReadFile(injectConfigFile) // nolint: vetshadow
				if err != nil {
					return err
				}
				var config inject.Config
				if err := yaml.Unmarshal(injectionConfig, &config); err != nil {
					return err
				}
				sidecarTemplate = config.Template
			} else {
				if sidecarTemplate, err = getInjectConfigFromConfigMap(kubeconfig); err != nil {
					return err
				}
			}

			var valuesConfig string
			if valuesFile != "" {
				valuesConfigBytes, err := ioutil.ReadFile(valuesFile) // nolint: vetshadow
				if err != nil {
					return err
				}
				valuesConfig = string(valuesConfigBytes)
			} else {
				if valuesConfig, err = getValuesFromConfigMap(kubeconfig); err != nil {
					return err
				}
			}

			if emitTemplate {
				config := inject.Config{
					Policy:   inject.InjectionPolicyEnabled,
					Template: sidecarTemplate,
				}
				out, err := yaml.Marshal(&config)
				if err != nil {
					return err
				}
				fmt.Println(string(out))
				return nil
			}

			return inject.IntoResourceFile(sidecarTemplate, valuesConfig, meshConfig, reader, writer)
		},
	}
)

const (
	defaultMeshConfigMapName   = "istio"
	defaultInjectConfigMapName = "istio-sidecar-injector"
)

func init() {
	rootCmd.AddCommand(injectCmd)

	injectCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfigFile", "",
		"mesh configuration filename. Takes precedence over --meshConfigMapName if set")
	injectCmd.PersistentFlags().StringVar(&injectConfigFile, "injectConfigFile", "",
		"injection configuration filename. Cannot be used with --injectConfigMapName")
	injectCmd.PersistentFlags().StringVar(&valuesFile, "valuesFile", "",
		"injection values configuration filename.")

	injectCmd.PersistentFlags().BoolVar(&emitTemplate, "emitTemplate", false,
		"Emit sidecar template based on parameterized flags")
	_ = injectCmd.PersistentFlags().MarkHidden("emitTemplate")

	injectCmd.PersistentFlags().StringVarP(&inFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	injectCmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")

	injectCmd.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", defaultMeshConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
	injectCmd.PersistentFlags().StringVar(&injectConfigMapName, "injectConfigMapName", defaultInjectConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio sidecar injection, key should be %q.", injectConfigMapKey))
}

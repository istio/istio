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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

const configMapKey = "mesh"

func getMeshConfigFromConfigMap(kubeconfig string) (*meshconfig.MeshConfig, error) {
	_, client, err := kube.CreateInterface(kubeconfig)
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
	yaml, exists := config.Data[configMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", configMapKey)
	}
	return model.ApplyMeshConfigDefaults(yaml)
}

var (
	hub             string
	tag             string
	sidecarProxyUID uint64
	verbosity       int
	versionStr      string // override build version
	enableCoreDump  bool
	imagePullPolicy string
	includeIPRanges string
	debugMode       bool
	emitTemplate    bool

	inFilename        string
	outFilename       string
	meshConfigFile    string
	meshConfigMapName string
	injectConfigFile  string
)

var (
	injectCmd = &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject Envoy sidecar into Kubernetes pod resources",
		Long: `

kube-inject manually injects envoy sidecar into kubernetes
workloads. Unsupported resources are left unmodified so it is safe to
run kube-inject over a single file that contains multiple Service,
ConfigMap, Deployment, etc. definitions for a complex application. Its
best to do this when the resource is initially created.

k8s.io/docs/concepts/workloads/pods/pod-overview/#pod-templates is
updated for Job, DaemonSet, ReplicaSet, and Deployment YAML resource
documents. Support for additional pod-based resource types can be
added as necessary.

The Istio project is continually evolving so the Istio sidecar
configuration may change unannounced. When in doubt re-run istioctl
kube-inject on deployments to get the most up-to-date changes.
`,
		Example: `
# Update resources on the fly before applying.
kubectl apply -f <(istioctl kube-inject -f <resource.yaml>)

# Create a persistent version of the deployment with Envoy sidecar
# injected.
istioctl kube-inject -f deployment.yaml -o deployment-injected.yaml

# Update an existing deployment.
kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -
`,
		PersistentPreRun: getRealKubeConfig,
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			switch {
			case inFilename != "" && emitTemplate:
				return errors.New("--filename and --emitTemplate are mutually exclusive")
			case inFilename == "" && !emitTemplate:
				return errors.New("filename not specified (see --filename or -f)")
			case meshConfigFile == "" && meshConfigMapName == "":
				return errors.New("--meshConfigFile or --meshConfigMapName must be set")
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
				writer = os.Stdout
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

			if versionStr == "" {
				versionStr = version.Info.String()
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
				sidecarTemplate, err = inject.GenerateTemplateFromParams(&inject.Params{
					InitImage:       inject.InitImageName(hub, tag, debugMode),
					ProxyImage:      inject.ProxyImageName(hub, tag, debugMode),
					Verbosity:       verbosity,
					SidecarProxyUID: sidecarProxyUID,
					Version:         versionStr,
					EnableCoreDump:  enableCoreDump,
					Mesh:            meshConfig,
					ImagePullPolicy: imagePullPolicy,
					IncludeIPRanges: includeIPRanges,
					DebugMode:       debugMode,
				})
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

			return inject.IntoResourceFile(sidecarTemplate, meshConfig, reader, writer)
		},
	}
)

func init() {
	rootCmd.AddCommand(injectCmd)

	injectCmd.PersistentFlags().StringVar(&hub, "hub", version.Info.DockerHub, "Docker hub")
	injectCmd.PersistentFlags().StringVar(&tag, "tag", version.Info.Version, "Docker tag")

	injectCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfigFile", "",
		"mesh configuration filename. Takes precedence over --meshConfigMapName if set")
	injectCmd.PersistentFlags().StringVar(&injectConfigFile, "injectConfigFile", "", "injection configuration filename")

	injectCmd.PersistentFlags().BoolVar(&emitTemplate, "emitTemplate", false, "Emit sidecar template based on parameterized flags")

	injectCmd.PersistentFlags().StringVarP(&inFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	injectCmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")
	injectCmd.PersistentFlags().IntVar(&verbosity, "verbosity",
		inject.DefaultVerbosity, "Runtime verbosity")
	injectCmd.PersistentFlags().Uint64Var(&sidecarProxyUID, "sidecarProxyUID",
		inject.DefaultSidecarProxyUID, "Envoy sidecar UID")
	injectCmd.PersistentFlags().StringVar(&versionStr, "setVersionString",
		"", "Override version info injected into resource")
	// Default --coreDump=true for pre-alpha development. Core dump
	// settings (i.e. sysctl kernel.*) affect all pods in a node and
	// require privileges. This option should only be used by the cluster
	// admin (see https://kubernetes.io/docs/concepts/cluster-administration/sysctl-cluster/)
	injectCmd.PersistentFlags().BoolVar(&enableCoreDump, "coreDump",
		true, "Enable/Disable core dumps in injected Envoy sidecar (--coreDump=true affects "+
			"all pods in a node and should only be used the cluster admin)")
	injectCmd.PersistentFlags().StringVar(&imagePullPolicy, "imagePullPolicy", inject.DefaultImagePullPolicy,
		"Sets the container image pull policy. Valid options are Always,IfNotPresent,Never."+
			"The default policy is IfNotPresent.")
	injectCmd.PersistentFlags().StringVar(&includeIPRanges, "includeIPRanges", "",
		"Comma separated list of IP ranges in CIDR form. If set, only redirect outbound "+
			"traffic to Envoy for IP ranges. Otherwise all outbound traffic is redirected")
	injectCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Use debug images and settings for the sidecar")

	injectCmd.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", "istio",
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
}

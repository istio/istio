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
	"os"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"

	"istio.io/pilot/platform/kube"
	"istio.io/pilot/platform/kube/inject"
	"istio.io/pilot/tools/version"
)

var (
	hub               string
	tag               string
	sidecarProxyUID   int64
	verbosity         int
	versionStr        string // override build version
	enableCoreDump    bool
	meshConfigMapName string
	imagePullPolicy   string
	includeIPRanges   string
	debugMode         bool

	inFilename  string
	outFilename string
)

var (
	injectCmd = &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject Envoy sidecar into Kubernetes pod resources",
		Long: `

Automatic Envoy sidecar injection via k8s admission controller is not
ready yet. Instead, use kube-inject to manually inject Envoy sidecar
into Kubernetes resource files. Unsupported resources are left
unmodified so it is safe to run kube-inject over a single file that
contains multiple Service, ConfigMap, Deployment, etc. definitions for
a complex application. Its best to do this when the resource is
initially created.

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
# injected. This is particularly useful to understand what is
# being injected before committing to Kubernetes API server.
istioctl kube-inject -f deployment.yaml -o deployment-with-istio.yaml

# Update an existing deployment.
kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -
`,
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			if inFilename == "" {
				return errors.New("filename not specified (see --filename or -f)")
			}

			var reader io.Reader
			if inFilename == "-" {
				reader = os.Stdin
			} else {
				if reader, err = os.Open(inFilename); err != nil {
					return err
				}
			}

			var writer io.Writer
			if outFilename == "" {
				writer = os.Stdout
			} else {
				var file *os.File
				if file, err = os.Create(outFilename); err != nil {
					return err
				}
				writer = file
				defer func() {
					// don't overwrite error if preceding injection failed
					errClose := file.Close()
					if err == nil {
						err = errClose
					}
				}()
			}

			if versionStr == "" {
				versionStr = version.Line()
			}

			_, client, err := kube.CreateInterface(kubeconfig)
			if err != nil {
				return err
			}

			_, meshConfig, err := inject.GetMeshConfig(client, namespace, meshConfigMapName)
			if err != nil {
				// Temporary hack (few days), until this is properly implemented
				// https://github.com/istio/pilot/issues/1153
				istioMeshConfigMap, istioMeshConfig, err := inject.GetMeshConfig(client,
					istioNamespace, meshConfigMapName)
				if err != nil {
					return fmt.Errorf("could not read valid configmap %q from namespace %q or %q: %v - "+
						"Re-run kube-inject with `-i <istioSystemNamespace> and ensure valid MeshConfig exists",
						meshConfigMapName, namespace, istioNamespace, err)
				}

				meshConfig = istioMeshConfig
				_, err = inject.CreateMeshConfigMap(client, namespace, meshConfigMapName, istioMeshConfigMap)
				if err != nil {
					return fmt.Errorf("cannot create Istio configuration map in namespace %s: %v",
						namespace, err)
				}
			}

			config := &inject.Config{
				Policy:     inject.DefaultInjectionPolicy,
				Namespaces: []string{v1.NamespaceAll},
				Params: inject.Params{
					InitImage:         inject.InitImageName(hub, tag, debugMode),
					ProxyImage:        inject.ProxyImageName(hub, tag, debugMode),
					Verbosity:         verbosity,
					SidecarProxyUID:   sidecarProxyUID,
					Version:           versionStr,
					EnableCoreDump:    enableCoreDump,
					Mesh:              meshConfig,
					MeshConfigMapName: meshConfigMapName,
					ImagePullPolicy:   imagePullPolicy,
					IncludeIPRanges:   includeIPRanges,
					DebugMode:         debugMode,
				},
			}
			return inject.IntoResourceFile(config, reader, writer)
		},
	}
)

func init() {
	rootCmd.AddCommand(injectCmd)

	injectCmd.PersistentFlags().StringVar(&hub, "hub", inject.DefaultHub, "Docker hub")
	injectCmd.PersistentFlags().StringVar(&tag, "tag", version.Info.Version, "Docker tag")

	injectCmd.PersistentFlags().StringVarP(&inFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	injectCmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")
	injectCmd.PersistentFlags().IntVar(&verbosity, "verbosity",
		inject.DefaultVerbosity, "Runtime verbosity")
	injectCmd.PersistentFlags().Int64Var(&sidecarProxyUID, "sidecarProxyUID",
		inject.DefaultSidecarProxyUID, "Envoy sidecar UID")
	injectCmd.PersistentFlags().StringVar(&versionStr, "setVersionString",
		"", "Override version info injected into resource")
	injectCmd.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", "istio",
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", inject.ConfigMapKey))

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
	injectCmd.PersistentFlags().BoolVar(&debugMode, "debug", true, "Use debug images and settings for the sidecar")
}

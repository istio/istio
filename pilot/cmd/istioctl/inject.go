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

	"istio.io/manager/cmd"
	"istio.io/manager/cmd/version"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/platform/kube/inject"

	"github.com/spf13/cobra"
)

const (
	// DefaultHubEnvVar is the environment variable that defaults the value of the --hub flag to istioctl kube-inject
	DefaultHubEnvVar = "MANAGER_HUB"
	// DefaultTagEnvVar is the environment variable that defaults the value of the --tag flag to istioctl kube-inject
	DefaultTagEnvVar = "MANAGER_TAG"
)

var (
	hub             string
	tag             string
	sidecarProxyUID int64
	verbosity       int
	versionStr      string // override build version
	enableCoreDump  bool
	meshConfig      string
	includeIPRanges string

	inFilename  string
	outFilename string

	kubeconfig string
	client     *kube.Client
	config     model.ConfigRegistry
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
			if hub == "" {
				return fmt.Errorf("specify --hub or define %v", DefaultHubEnvVar)
			}
			if tag == "" {
				return fmt.Errorf("specify --tag or define %v", DefaultTagEnvVar)
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
				defer func() { err = file.Close() }()
			}

			if versionStr == "" {
				versionStr = version.Line()
			}

			mesh, err := cmd.GetMeshConfig(client.GetKubernetesClient(), namespace, meshConfig)
			if err != nil {
				return fmt.Errorf("Istio configuration not found. Verify istio configmap is "+
					"installed in namespace %q with `kubectl get -n %s configmap istio`",
					namespace, namespace)
			}
			params := &inject.Params{
				InitImage:       inject.InitImageName(hub, tag),
				ProxyImage:      inject.ProxyImageName(hub, tag),
				Verbosity:       verbosity,
				SidecarProxyUID: sidecarProxyUID,
				Version:         versionStr,
				EnableCoreDump:  enableCoreDump,
				Mesh:            mesh,
				IncludeIPRanges: includeIPRanges,
			}
			if meshConfig != cmd.DefaultConfigMapName {
				params.MeshConfigMapName = meshConfig
			}
			return inject.IntoResourceFile(params, reader, writer)
		},
	}
)

func init() {
	rootCmd.AddCommand(injectCmd)

	// Order of precedence for setting docker hub/tag is flags,
	// envvars, and then compiled in defaults.
	defaultHub := version.KubeInjectHub
	if v := os.Getenv(DefaultHubEnvVar); v != "" {
		defaultHub = v
	}
	injectCmd.PersistentFlags().StringVar(&hub, "hub", defaultHub, "Docker hub")

	defaultTag := version.KubeInjectTag
	if v := os.Getenv(DefaultTagEnvVar); v != "" {
		defaultTag = v
	}
	injectCmd.PersistentFlags().StringVar(&tag, "tag", defaultTag, "Docker tag")

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
	injectCmd.PersistentFlags().StringVar(&meshConfig, "meshConfig", cmd.DefaultConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", cmd.ConfigMapKey))

	// Default --coreDump=true for pre-alpha development. Core dump
	// settings (i.e. sysctl kernel.*) affect all pods in a node and
	// require privileges. This option should only be used by the cluster
	// admin (see https://kubernetes.io/docs/concepts/cluster-administration/sysctl-cluster/)
	injectCmd.PersistentFlags().BoolVar(&enableCoreDump, "coreDump",
		true, "Enable/Disable core dumps in injected Envoy sidecar (--coreDump=true affects "+
			"all pods in a node and should only be used the cluster admin)")
	injectCmd.PersistentFlags().StringVar(&includeIPRanges, "includeIPRanges", "",
		"Comma separated list of IP ranges in CIDR form. If set, only redirect outbound "+
			"traffic to Envoy for IP ranges. Otherwise all outbound traffic is redirected")
}

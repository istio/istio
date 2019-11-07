// Copyright 2018 Istio Authors
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

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/compare"
	sdscompare "istio.io/istio/istioctl/pkg/writer/compare/sds"
	"istio.io/istio/istioctl/pkg/writer/pilot"
)

var (
	sdsDump bool
	sdsJSON bool
)

func statusCommand() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "proxy-status [<pod-name[.namespace]>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Pilot to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync diff for a single Envoy and Pilot
	istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := clientExecSdsFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
				path := fmt.Sprintf("config_dump")
				envoyDump, err := kubeClient.EnvoyDo(podName, ns, "GET", path, nil)
				if err != nil {
					return err
				}

				// when sds flag set, should diff node agent secrets with active Envoy secrets
				if sdsDump {
					var outputFormat sdscompare.Format
					if sdsJSON {
						outputFormat = sdscompare.JSON
					} else {
						outputFormat = sdscompare.TABULAR
					}
					writer := sdscompare.NewSDSWriter(c.OutOrStdout(), outputFormat)
					return sdsDiff(kubeClient, writer, podName, ns)
				}

				path = fmt.Sprintf("/debug/config_dump?proxyID=%s.%s", podName, ns)
				pilotDumps, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", path, nil)
				if err != nil {
					return err
				}
				c, err := compare.NewComparator(c.OutOrStdout(), pilotDumps, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}
			statuses, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/syncz", nil)
			if err != nil {
				return err
			}
			sw := pilot.StatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(statuses)
		},
	}

	statusCmd.Flags().BoolVarP(&sdsDump, "sds", "s", false,
		"(experimental) Retrieve synchronization between active secrets on Envoy instance with those on corresponding node agents")
	statusCmd.Flags().BoolVar(&sdsJSON, "sds-json", false,
		"Determines whether SDS dump outputs JSON")

	return statusCmd
}

// sdsDiff diffs pod secrets with corresponding node agent secrets
func sdsDiff(
	c kubernetes.ExecClientSDS, w sdscompare.SDSWriter, podName, namespace string) error {
	nodeAgentSecretItems, err := c.GetPodNodeAgentSecrets(podName, namespace, istioNamespace)
	if err != nil {
		return fmt.Errorf("failed to retrieve node agent secrets: %v", err)
	}
	envoyDump, err := c.EnvoyDo(podName, namespace, "GET", "config_dump", nil)
	if err != nil {
		return fmt.Errorf("could not get sidecar config dump for %s.%s: %v",
			podName, namespace, err)
	}

	comparator, err := sdscompare.NewSDSComparator(
		w, nodeAgentSecretItems, envoyDump, podName)
	if err != nil {
		return fmt.Errorf("failed to create SDS comparator: %v", comparator)
	}

	return comparator.Diff()
}

func newExecClient(kubeconfig, configContext string) (kubernetes.ExecClient, error) {
	return kubernetes.NewClient(kubeconfig, configContext)
}

func newSDSExecClient(kubeconfig, configContext string) (kubernetes.ExecClientSDS, error) {
	return kubernetes.NewClient(kubeconfig, configContext)
}

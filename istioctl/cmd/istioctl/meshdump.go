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

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pkg/log"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
)

var (
	outMeshDumpFilename string
	dumpPilot           bool
	dumpEnvoy           bool

	meshDumpCmd = &cobra.Command{
		Use:   "mesh-dump",
		Short: "Creates an archive of mesh information for debugging purposes [kube only]",
		Long: `
Creates an archive of mesh information required for debugging.
`,
		Example: `# Create archive of mesh information for debugging
    istioctl mesh-dump
`,
		Aliases: []string{"md"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := newExecClient(kubeconfig, configContext)
			if err != nil {
				return err
			}

			f, err := os.Create(outMeshDumpFilename)
			if err != nil {
				return err
			}

			zipper := gzip.NewWriter(f)
			tarWriter := tar.NewWriter(zipper)
			defer func() {
				tarWriter.Close()
				zipper.Close()
				f.Close()
			}()

			var errs error
			var pilotADSz map[string][]byte
			if dumpPilot || dumpAll() {
				pilotADSz, err = kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/adsz", nil)
				if err != nil {
					log.Errorf("error retrieving adsz from Pilots: %v", err)
				}
				log.Debugf("Finished AllPilotsDiscoveryDo with %d pilots", len(pilotADSz))
				for pilot, adsz := range pilotADSz {
					fname := filepath.Join("pilot", pilot+".json")
					if err = writeToTar(tarWriter, fname, adsz); err != nil {
						errs = multierror.Append(errs, err)
					}
				}
			}

			var pods []v1.Pod
			if dumpEnvoy || dumpAll() {
				pods, err = kubeClient.GetPods("", "")
				if err != nil {
					errs = multierror.Append(errs, err)
				}
				for _, pod := range pods {
					_, err := kubernetes.GetPilotAgentContainer(pod)
					if err != nil {
						log.Debugf("Skipping pod %s/%s which has no agent-container", pod.Namespace, pod.Name)
						continue // Ignore pods that have no pilot-agent
					}
					log.Debugf("Querying pod %s/%s", pod.Namespace, pod.Name)
					out, err := kubeClient.EnvoyDo(pod.Name, pod.Namespace, "GET", "/config_dump", nil)
					if err != nil {
						errs = multierror.Append(errs, err)
					}
					if len(out) > 0 {
						fname := filepath.Join("pod", pod.Namespace, fmt.Sprintf("%s.json", pod.Name))
						if err = writeToTar(tarWriter, fname, out); err != nil {
							errs = multierror.Append(errs, err)
						}
					}
				}
			}

			c.Printf("Wrote %d pilot configurations and %d pod configurations to %s\n", len(pilotADSz), len(pods), outMeshDumpFilename)

			return errs
		},
	}
)

func init() {
	experimentalCmd.AddCommand(meshDumpCmd)
	meshDumpCmd.PersistentFlags().StringVarP(&outMeshDumpFilename, "output", "o", "istio-dump.tgz", "Output .tgz filename")
	meshDumpCmd.PersistentFlags().BoolVar(&dumpPilot, "pilot", false, "Mesh Dump of Pilot status")
	meshDumpCmd.PersistentFlags().BoolVar(&dumpEnvoy, "envoy", false, "Mesh Dump of Envoy status")
}

// writeToTar writes a single file to a tar/tgz archive.
func writeToTar(out *tar.Writer, name string, body []byte) error {
	h := &tar.Header{
		Name:    name,
		Size:    int64(len(body)),
		Mode:    0444,
		ModTime: time.Now(),
	}
	if err := out.WriteHeader(h); err != nil {
		return err
	}
	if _, err := out.Write(body); err != nil {
		return err
	}
	return nil
}

func dumpAll() bool {
	// If nothing has been explicitly requested, dump everything
	return !dumpPilot && !dumpEnvoy
}

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
	"os"
	"path/filepath"
	"time"

	"istio.io/istio/pkg/log"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
)

var (
	outMeshDumpFilename string

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
			noOutput := false
			defer func() {
				tarWriter.Close()
				zipper.Close()
				f.Close()
				if noOutput {
					os.Remove(outMeshDumpFilename)
				}
			}()

			var errs error
			pilotADSz, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/adsz", nil)
			if err != nil {
				log.Errorf("error retrieving adsz from Pilots: %v", err)
			}
			log.Debugf("Finished ADS query with %d pilots", len(pilotADSz))
			for pilot, adsz := range pilotADSz {
				fname := filepath.Join("adsz", pilot+".json")
				if err = writeToTar(tarWriter, fname, adsz); err != nil {
					errs = multierror.Append(errs, err)
				}
			}

			pilotSyncz, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/syncz", nil)
			if err != nil {
				log.Errorf("error retrieving syncz from Pilots: %v", err)
			}
			log.Debugf("Finished Sync query with %d pilots", len(pilotADSz))
			for pilot, syncz := range pilotSyncz {
				fname := filepath.Join("syncz", pilot+".json")
				if err = writeToTar(tarWriter, fname, syncz); err != nil {
					errs = multierror.Append(errs, err)
				}
			}

			if len(pilotADSz) == 0 && len(pilotSyncz) == 0 {
				noOutput = true // deferred closer will delete empty .tgz file
			}

			if !noOutput {
				c.Printf("Wrote %d pilot Aggregate (adsz) configurations and %d Sync (syncz) configurations to %s\n", len(pilotADSz), len(pilotSyncz), outMeshDumpFilename)
			}

			return errs
		},
	}
)

func init() {
	experimentalCmd.AddCommand(meshDumpCmd)
	meshDumpCmd.PersistentFlags().StringVarP(&outMeshDumpFilename, "output", "o", "istio-dump.tgz", "Output .tgz filename")
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

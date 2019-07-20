// Copyright 2019 Istio Authors
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

package iop

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/manifest"

	"istio.io/pkg/log"
)

func manifestCmd(rootArgs *rootArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "manifest",
		Short: "Generates Istio install manifest.",
		Long:  "The manifest subcommand is used to generate an Istio install manifest based on the input CR.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			writeManifests(rootArgs)
		}}

}

func writeManifests(args *rootArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	manifests, err := genManifests(args)
	if err != nil {
		log.Fatalf("Could not generate manifests: %s", err)
	}

	if args.outFilename == "" {
		for _, m := range manifests {
			fmt.Println(m)
		}
	} else {
		if err := os.MkdirAll(args.outFilename, os.ModePerm); err != nil {
			log.Fatalf("Could not create output folder: %s", err)
		}
		if err := manifest.RenderToDir(manifests, args.outFilename, args.dryRun, args.verbose); err != nil {
			log.Errorf("Could not render manifests to output folder: %s", err)
		}
	}
}

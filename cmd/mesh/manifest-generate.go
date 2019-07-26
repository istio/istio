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

package mesh

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/manifest"
)

type manifestGenerateArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// outFilename is the path to the generated output filename.
	outFilename string
	// set is a string with element format "path=value" where path is an IstioControlPlane path and the value is a
	// value to set the node at that path to.
	set []string
}

func addManifestGenerateFlags(cmd *cobra.Command, args *manifestGenerateArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.outFilename, "output", "o", "", "Manifest output directory path.")
	cmd.PersistentFlags().StringSliceVarP(&args.set, "set", "s", nil, setFlagHelpStr)
}

func manifestGenerateCmd(rootArgs *rootArgs, mgArgs *manifestGenerateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generates Istio install manifest.",
		Long:  "The generate subcommand is used to generate an Istio install manifest.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			manifestGenerate(rootArgs, mgArgs)
		}}

}

func manifestGenerate(args *rootArgs, mgArgs *manifestGenerateArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	overlayFromSet, err := makeTreeFromSetList(mgArgs.set)
	if err != nil {
		logAndFatalf(args, err.Error())
	}
	manifests, err := genManifests(mgArgs.inFilename, overlayFromSet)
	if err != nil {
		logAndFatalf(args, err.Error())
	}

	if mgArgs.outFilename == "" {
		for _, m := range manifests {
			fmt.Println(m)
		}
	} else {
		if err := os.MkdirAll(mgArgs.outFilename, os.ModePerm); err != nil {
			logAndFatalf(args, err.Error())
		}
		if err := manifest.RenderToDir(manifests, mgArgs.outFilename, args.dryRun, args.verbose); err != nil {
			logAndFatalf(args, err.Error())
		}
	}
}

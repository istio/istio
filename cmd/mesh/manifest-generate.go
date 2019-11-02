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
	"sort"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/name"
)

type manifestGenerateArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// outFilename is the path to the generated output directory.
	outFilename string
	// set is a string with element format "path=value" where path is an IstioControlPlane path and the value is a
	// value to set the node at that path to.
	set []string
	// force proceeds even if there are validation errors
	force bool
}

func addManifestGenerateFlags(cmd *cobra.Command, args *manifestGenerateArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.outFilename, "output", "o", "", "Manifest output directory path")
	cmd.PersistentFlags().StringSliceVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
}

func manifestGenerateCmd(rootArgs *rootArgs, mgArgs *manifestGenerateArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generates an Istio install manifest",
		Long:  "The generate subcommand generates an Istio install manifest and outputs to the console by default.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("generate accepts no positional arguments, got %#v", args)
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			manifestGenerate(rootArgs, mgArgs, l)
		}}

}

func manifestGenerate(args *rootArgs, mgArgs *manifestGenerateArgs, l *logger) {
	if err := configLogs(args.logToStdErr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	overlayFromSet, err := makeTreeFromSetList(mgArgs.set, mgArgs.force)
	if err != nil {
		l.logAndFatal(err.Error())
	}
	manifests, err := genManifests(mgArgs.inFilename, overlayFromSet, mgArgs.force, l)
	if err != nil {
		l.logAndFatal(err.Error())
	}

	if mgArgs.outFilename == "" {
		for _, m := range orderedManifests(manifests) {
			l.print(m + "\n")
		}
	} else {
		if err := os.MkdirAll(mgArgs.outFilename, os.ModePerm); err != nil {
			l.logAndFatal(err.Error())
		}
		if err := manifest.RenderToDir(manifests, mgArgs.outFilename, args.dryRun); err != nil {
			l.logAndFatal(err.Error())
		}
	}
}

func orderedManifests(mm name.ManifestMap) []string {
	var keys, out []string
	for k := range mm {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, mm[name.ComponentName(k)])
	}
	return out
}

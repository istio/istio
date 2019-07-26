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
	"strings"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

type manifestApplyArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// set is a string with element format "path=value" where path is an IstioControlPlane path and the value is a
	// value to set the node at that path to.
	set []string
}

func addManifestApplyFlags(cmd *cobra.Command, args *manifestApplyArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config.")
	cmd.PersistentFlags().StringSliceVarP(&args.set, "set", "s", nil, setFlagHelpStr)
}

func manifestApplyCmd(rootArgs *rootArgs, maArgs *manifestApplyArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Generates and applies Istio install manifest.",
		Long:  "The apply subcommand is used to generate an Istio install manifest and apply it to a cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			manifestApply(rootArgs, maArgs)
		}}

}

func manifestApply(args *rootArgs, maArgs *manifestApplyArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	overlayFromSet, err := makeTreeFromSetList(maArgs.set)
	if err != nil {
		logAndFatalf(args, err)
	}
	manifests, err := genManifests(maArgs.inFilename, overlayFromSet)
	if err != nil {
		logAndFatalf(args, "Could not generate manifest: %v", err)
	}

	out, err := manifest.ApplyAll(manifests, version.NewVersion("", 1, 2, 0, ""), args.dryRun, args.verbose)
	if err != nil {
		logAndFatalf(args, "Failed to apply manifest with kubectl client: %v", err)
	}

	for cn := range manifests {
		cs := fmt.Sprintf("CompositeOutput for component %s:", cn)
		log.Infof("\n%s\n%s", cs, strings.Repeat("=", len(cs)))
		if out.Err[cn] != nil {
			logAndPrintf(args, "Error object: %s\n", out.Err[cn])
		}
		if strings.TrimSpace(out.Stderr[cn]) != "" {
			logAndPrintf(args, "Error string:\n%s\n", out.Stderr[cn])
		}
		if strings.TrimSpace(out.Stdout[cn]) != "" {
			logAndPrintf(args, "Output:\n%s\n", out.Stdout[cn])
		}
	}
}

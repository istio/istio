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
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/tpath"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/component"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

type dumpArgs struct {
	// profile determines which profile to display the config for.
	profile string
	// If set, display the translated Helm values rather than IstioControlPlaneSpec.
	helmValues bool
	// rootConfigNode sets the root node for the subtree to display the config for.
	rootConfigNode string
}

func addDumpFlags(cmd *cobra.Command, dumpArgs *dumpArgs) {
	cmd.PersistentFlags().StringVarP(&dumpArgs.profile, "profile", "p", "",
		"The profile to dump the config for.  If set, -f cannot be set.")
	cmd.PersistentFlags().StringVarP(&dumpArgs.rootConfigNode, "root-config-node", "r", "",
		"The path the root of the configuration subtree to dump e.g. trafficManagement.components.pilot. By default, dump whole tree. ")
	cmd.PersistentFlags().BoolVarP(&dumpArgs.helmValues, "helm-values", "", false,
		"If set, dumps the Helm values that IstioControlPlaceSpec is translated to before manifests are rendered.")
}

func dumpProfileDefaultsCmd(rootArgs *rootArgs, dumpArgs *dumpArgs) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-profile",
		Short: "Dump default values for the profile passed in the CR.",
		Long:  "The dump-profile subcommand is used to dump default values for the profile passed in the CR.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			dumpProfile(rootArgs, dumpArgs)
		}}
	return cmd
}

func dumpProfile(args *rootArgs, dumpArgs *dumpArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	if args.inFilename != "" && dumpArgs.profile != "" {
		log.Fatalf("Cannot set both --profile and --filename")
	}

	writer, err := getWriter(args)
	if err != nil {
		log.Fatalf("Could not create output writer: %s", err)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			log.Fatalf("Did not close output successfully: %v", err)
		}
	}()

	mergedYAML := ""
	mergedICPS := &v1alpha2.IstioControlPlaneSpec{}

	switch {
	case dumpArgs.profile != "":
		mergedYAML, err = helm.ReadValuesYAML(dumpArgs.profile)
		if err != nil {
			log.Fatalf("Failed to read YAML from profile: %v", err)
		}
	default:
		overlayYAML := ""
		if args.inFilename != "" {
			b, err := ioutil.ReadFile(args.inFilename)
			if err != nil {
				log.Fatalf("Could not read from file %s: %v", args.inFilename, err)
			}
			overlayYAML = string(b)
		}

		// Start with unmarshaling and validating the user CR (which is an overlay on the base profile).
		overlayICPS := &v1alpha2.IstioControlPlaneSpec{}
		if err := util.UnmarshalWithJSONPB(overlayYAML, overlayICPS); err != nil {
			log.Fatalf("Could not unmarshal the input file: %s\n\nOriginal YAML:\n%s\n", err, overlayYAML)
		}
		if errs := validate.CheckIstioControlPlaneSpec(overlayICPS, false); len(errs) != 0 {
			log.Fatalf("Input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s\n", errs, overlayYAML)
		}

		// Now read the base profile specified in the user spec.
		fname, err := helm.FilenameFromProfile(overlayICPS.Profile)
		if err != nil {
			log.Fatalf("Could not get filename from profile: %s", err)
		}

		baseYAML, err := helm.ReadValuesYAML(overlayICPS.Profile)
		if err != nil {
			log.Fatalf("Could not read the profile values for %s: %s", fname, err)
		}

		mergedYAML, err = helm.OverlayYAML(baseYAML, overlayYAML)
		if err != nil {
			log.Fatalf("Could not overlay user config over base: %s", err)
		}
		// Now unmarshal and validate the combined base profile and user CR overlay.
		if err := util.UnmarshalWithJSONPB(mergedYAML, mergedICPS); err != nil {
			log.Fatalf("Merged spec failed validation against IstioControlPlaneSpec: \n%v\n", mergedICPS)
		}
		if errs := validate.CheckIstioControlPlaneSpec(mergedICPS, true); len(errs) != 0 {
			log.Fatalf(err.Error())
		}
	}

	t, err := translate.NewTranslator(version.NewMinorVersion(1, 2))
	if err != nil {
		log.Fatalf("Failed to create translator: %v", err)
	}

	if dumpArgs.helmValues {
		mergedYAML, err = component.CompileHelmValues(mergedICPS, t, "")
		if err != nil {
			log.Fatalf("Merged spec failed validation against IstioControlPlaneSpec: \n%v", mergedICPS)
		}
	}

	finalYAML, err := getConfigSubtree(mergedYAML, dumpArgs.rootConfigNode)
	if err != nil {
		log.Fatalf("Failed to get root config from merged YAML: %v", err)
	}

	if _, err := writer.WriteString(finalYAML); err != nil {
		log.Fatalf("Could not write values; %s", err)
	}
}

func getConfigSubtree(manifest, path string) (string, error) {
	root := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(manifest), &root); err != nil {
		return "", err
	}

	nc, _, err := tpath.GetPathContext(root, util.PathFromString(path))
	if err != nil {
		return "", err
	}
	out, err := yaml.Marshal(nc.Node)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

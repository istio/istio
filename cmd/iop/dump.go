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

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	"istio.io/pkg/log"
)

func dumpProfileDefaultsCmd(rootArgs *rootArgs) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-profile",
		Short: "Dump default values for the profile passed in the CR.",
		Long:  "The dump-profile subcommand is used to dump default values for the profile passed in the CR.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			dumpProfile(rootArgs)
		}}
	return cmd
}

func dumpProfile(args *rootArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	overlayYAML := ""
	if args.inFilename != "" {
		b, err := ioutil.ReadFile(args.inFilename)
		if err != nil {
			log.Fatalf(err.Error())
		}
		overlayYAML = string(b)
	}

	writer, err := getWriter(args)
	if err != nil {
		log.Fatalf(err.Error())
	}
	defer func() {
		if err := writer.Close(); err != nil {
			log.Errorf("Did not close output successfully: %v", err)
		}
	}()

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
		log.Fatalf(err.Error())
	}

	baseYAML, err := helm.ReadValuesYAML(overlayICPS.Profile)
	if err != nil {
		log.Fatalf("Could not read the profile values for %s: %s", fname, err)
	}

	if _, err := writer.WriteString(baseYAML); err != nil {
		log.Fatalf(err.Error())
	}
}

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
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

func installCmd(rootArgs *rootArgs) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Installs Istio to cluster.",
		Long:  "The install subcommand is used to install Istio into a cluster, given a CR path. ",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			installManifests(rootArgs)
		}}

	return cmd
}

func installManifests(args *rootArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}

	manifests, err := genManifests(args)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if err := manifest.ApplyAll(manifests, version.NewVersion("", 1, 2, 0, ""), args.dryRun, args.verbose); err != nil {
		log.Fatalf(err.Error())
	}
}

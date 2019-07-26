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

	"github.com/spf13/cobra"
)

func manifestMigrateCmd(rootArgs *rootArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrates a file containing Helm values to IstioControlPlane format.",
		Long:  "The migrate subcommand is used to migrate a configuration in Helm values format to IstioControlPlane format.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			manifestMigrate(rootArgs)
		}}

}

func manifestMigrate(_ *rootArgs) {
	fmt.Println("Not yet implemented.")
}

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

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	istioVersion "istio.io/pkg/version"
)

func newVersionCommand() *cobra.Command {
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{GetRemoteVersion: getRemoteInfo})
	versionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "short" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
		if flag.Name == "remote" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
	})
	return versionCmd
}

func getRemoteInfo() (*istioVersion.MeshInfo, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.GetIstioVersions(istioNamespace)
}

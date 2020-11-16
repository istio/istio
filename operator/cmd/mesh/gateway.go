// Copyright Istio Authors
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
	"github.com/spf13/cobra"
)

// GatewayCmd is a group of commands related to installation and management of the ingress gateway.
func GatewayCmd() *cobra.Command {
	gc := &cobra.Command{
		Use:   "gateway",
		Short: "Commands related to Istio gateway.",
		Long:  "The gateway command that installs the data plane of Istio by creating the new ingress gateway.",
	}

	oic := operatorGatewayInstallCmd()
	gc.AddCommand(oic)

	return gc
}

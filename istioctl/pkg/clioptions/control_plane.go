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

package clioptions

import "github.com/spf13/cobra"

// ControlPlaneOptions defines common options used by istioctl.
type ControlPlaneOptions struct {
	// Revision is the istio.io/rev control plane revision
	Revision string
}

// AttachControlPlaneFlags attaches control-plane flags to a Cobra command.
// (Currently just --revision)
func (o *ControlPlaneOptions) AttachControlPlaneFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&o.Revision, "revision", "",
		"control plane revision")
}

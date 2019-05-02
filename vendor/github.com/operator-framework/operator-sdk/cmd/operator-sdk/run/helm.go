// Copyright 2019 The Operator-SDK Authors
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

package run

import (
	"github.com/operator-framework/operator-sdk/pkg/helm"
	hoflags "github.com/operator-framework/operator-sdk/pkg/helm/flags"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"

	"github.com/spf13/cobra"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// newRunHelmCmd returns a command that will run a helm operator.
func newRunHelmCmd() *cobra.Command {
	var flags *hoflags.HelmOperatorFlags
	runHelmCmd := &cobra.Command{
		Use:   "helm",
		Short: "Runs as a helm operator",
		Long: `Runs as a helm operator. This is intended to be used when running
in a Pod inside a cluster. Developers wanting to run their operator locally
should use "up local" instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logf.SetLogger(zap.Logger())

			return helm.Run(flags)
		},
	}
	flags = hoflags.AddTo(runHelmCmd.Flags())

	return runHelmCmd
}

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

package generate

import (
	"fmt"

	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/internal/genutil"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	"github.com/spf13/cobra"
)

func newGenerateK8SCmd() *cobra.Command {
	k8sCmd := &cobra.Command{
		Use:   "k8s",
		Short: "Generates Kubernetes code for custom resource",
		Long: `k8s generator generates code for custom resources given the API
specs in pkg/apis/<group>/<version> directories to comply with kube-API
requirements. Go code is generated under
pkg/apis/<group>/<version>/zz_generated.deepcopy.go.
Example:
	$ operator-sdk generate k8s
	$ tree pkg/apis
	pkg/apis/
	└── app
		└── v1alpha1
			├── zz_generated.deepcopy.go
`,
		RunE: k8sFunc,
	}

	k8sCmd.Flags().StringVar(&headerFile, "header-file", "", "Path to file containing headers for generated files.")

	return k8sCmd
}

func k8sFunc(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command %s doesn't accept any arguments", cmd.CommandPath())
	}

	// Only Go projects can generate k8s deepcopy code.
	if err := projutil.CheckGoProjectCmd(cmd); err != nil {
		return err
	}

	return genutil.K8sCodegen(headerFile)
}

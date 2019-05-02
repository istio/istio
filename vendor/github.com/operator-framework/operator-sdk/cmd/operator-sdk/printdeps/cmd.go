// Copyright 2018 The Operator-SDK Authors
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

package printdeps

import (
	"fmt"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"

	"github.com/spf13/cobra"
)

var asFile bool

func NewCmd() *cobra.Command {
	printDepsCmd := &cobra.Command{
		Use:   "print-deps",
		Short: "Print Golang packages and versions required to run the operator",
		Long: `The operator-sdk print-deps command prints all Golang packages and versions expected
by this version of the Operator SDK. Versions for these packages should match
those in an operators' Gopkg.toml file.

print-deps prints in columnar format by default. Use the --as-file flag to
print in Gopkg.toml file format.
`,
		RunE: printDepsFunc,
	}

	printDepsCmd.Flags().BoolVar(&asFile, "as-file", false, "Print dependencies in Gopkg.toml file format.")

	return printDepsCmd
}

func printDepsFunc(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command %s doesn't accept any arguments", cmd.CommandPath())
	}
	if asFile {
		scaffold.PrintDepsAsFile()
	} else if err := scaffold.PrintDeps(); err != nil {
		return fmt.Errorf("print deps failed: (%v)", err)
	}
	return nil
}

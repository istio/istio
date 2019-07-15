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
	"path/filepath"

	"istio.io/istio/galley/pkg/config/analysis/local"

	"github.com/spf13/cobra"
)

// Analyze command
func Analyze() *cobra.Command {
	return &cobra.Command{
		Use:     "analyze",
		Short:   "Analyze Istio configuration",
		Example: `istioctl experimental analyze <directory>`,
		RunE: func(cmd *cobra.Command, args []string) error {

			files, err := gatherFiles(args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})
			messages, err := local.AnalyzeFiles("svc.local", cancel, files...)
			if err != nil {
				return err
			}

			for _, m := range messages {
				fmt.Printf("%v\n", m.String())
			}

			return nil
		},
	}
}

func gatherFiles(args []string) ([]string, error) {
	var result []string
	for _, a := range args {
		paths, err := filepath.Glob(a)
		if err != nil {
			return nil, err
		}
		result = append(result, paths...)
	}
	return result, nil
}

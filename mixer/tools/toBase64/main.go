// Copyright 2018 Istio Authors
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

package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func withArgs(args []string, errorf func(format string, a ...interface{})) {
	var output string

	rootCmd := cobra.Command{
		Use:   "tobase64",
		Short: "converts the content of a file to base64 encoded string",
		Long:  "Usage: tobase64 path-to-input-file -o path-to-output-file",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) <= 0 {
				errorf("must specify an input file.")
				return
			}

			inPath, _ := filepath.Abs(args[0])
			byts, err := ioutil.ReadFile(inPath)
			if err != nil {
				errorf("invalid path %s. %v", output, err)
				return
			}

			outPath := ""
			if output != "" {
				outPath, _ = filepath.Abs(output)
			}
			str := base64.StdEncoding.EncodeToString(byts)
			if outPath != "" {
				if err := ioutil.WriteFile(outPath, []byte(str), 0644); err != nil {
					errorf("cannot write to output file '%s': %v", outPath, err)
					return
				}
			} else {
				fmt.Println(str)
			}
		},
	}

	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "name of the output"+
		" file. If not specified, the result base64 string will be printed to standard out")

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
	}
}

func main() {
	withArgs(os.Args[1:],
		func(format string, a ...interface{}) {
			fmt.Fprintf(os.Stderr, format+"\n", a...) // nolint: gas
			os.Exit(1)
		})
}

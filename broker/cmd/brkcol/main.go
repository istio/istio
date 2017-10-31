// Copyright 2017 Istio Authors
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

// This is a simple command that is used to output the auto-generated collateral
// files for the various mixer CLI commands. More specifically, this outputs
// markdown files and man pages that describe the CLI commands, along with
// bash completion files.

package main

import (
	"os"

	"istio.io/istio/broker/cmd/brkcol/cmd"
	"istio.io/istio/broker/cmd/shared"
)

func main() {
	rootCmd := cmd.GetRootCmd(os.Args[1:], shared.Printf, shared.Fatalf)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

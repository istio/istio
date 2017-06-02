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

// Package shared contains types and functions that are used across the full
// set of broker commands.
package shared

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/broker/pkg/version"
)

// FormatFn formats the supplied arguments according to the format string
// provided and executes some set of operations with the result.
type FormatFn func(format string, args ...interface{})

// Fatalf is a FormatFn that prints the formatted string to os.Stderr and then
// calls os.Exit().
var Fatalf = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(-1)
}

// Printf is a FormatFn that prints the formatted string to os.Stdout.
var Printf = func(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// VersionCmd is a command used to print version information.
func VersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Prints out build version information",
		Run: func(cmd *cobra.Command, args []string) {
			Printf("%s", version.Info)
		},
	}
}

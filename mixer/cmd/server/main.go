// Copyright 2016 Istio Authors
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
	"flag"
	"os"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	_ "google.golang.org/grpc/grpclog/glogger"
)

// A function used for error output.
type errorFn func(format string, a ...interface{})

// withArgs is like main except that it is parameterized with the
// command-line arguments to use, along with a function to call
// in case of errors. This allows the function to be invoked
// from test code.
func withArgs(args []string, errorf errorFn) {
	rootCmd := cobra.Command{
		Use:   "mixs",
		Short: "The Istio mixer provides control plane functionality to the Istio proxy and services",
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	// hack to make flag.Parsed return true such that glog is happy
	// about the flags having been parsed
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	/* #nosec */
	_ = fs.Parse([]string{})
	flag.CommandLine = fs

	rootCmd.AddCommand(adapterCmd(errorf))
	rootCmd.AddCommand(serverCmd(errorf))

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
	}
}

func main() {
	withArgs(os.Args[1:],
		func(format string, a ...interface{}) {
			glog.Errorf(format, a...)
			os.Exit(1)
		})
}

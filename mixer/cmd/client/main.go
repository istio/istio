// Copyright 2016 Google Inc.
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

	"github.com/spf13/cobra"
)

type rootArgs struct {
	// stringAttributes is the list of name/value pairs of string attributes that will be sent with requests.
	stringAttributes string

	// int64Attributes is the list of name/value pairs of int64 attributes that will be sent with requests.
	int64Attributes string

	// float64Attributes is the list of name/value pairs of float64 attributes that will be sent with requests.
	doubleAttributes string

	// boolAttributes is the list of name/value pairs of bool attributes that will be sent with requests.
	boolAttributes string

	// timestampAttributes is the list of name/value pairs of timestamp attributes that will be sent with requests.
	timestampAttributes string

	// bytesAttributes is the list of name/value pairs of bytes attributes that will be sent with requests.
	bytesAttributes string

	// mixerAddress is the full address (including port) of a mixer instance to call.
	mixerAddress string
}

func main() {
	// RootCmd represents the base command when called without any subcommands
	rootCmd := &cobra.Command{
		Use:   "client",
		Short: "Invoke the API of a running instance of the Istio mixer",
	}

	rootArgs := &rootArgs{}

	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)
	rootCmd.PersistentFlags().StringVarP(&rootArgs.mixerAddress, "mixer", "m", "localhost:9091", "Address and port of running instance of the mixer")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.stringAttributes, "string_attributes", "s", "", "List of name/value string attributes specified as name1=value1,name2=value2,...")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.int64Attributes, "int64_attributes", "i", "", "List of name/value int64 attributes specified as name1=value1,name2=value2,...")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.doubleAttributes, "double_attributes", "d", "", "List of name/value float64 attributes specified as name1=value1,name2=value2,...")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.boolAttributes, "bool_attributes", "b", "", "List of name/value bool attributes specified as name1=value1,name2=value2,...")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.timestampAttributes, "timestamp_attributes", "t", "", "List of name/value timestamp attributes specified as name1=value1,name2=value2,...")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.bytesAttributes, "bytes_attributes", "", "", "List of name/value bytes attributes specified as name1=b0:b1:b3,name2=b4:b5:b6,...")

	rootCmd.AddCommand(checkCmd(rootArgs))
	rootCmd.AddCommand(reportCmd(rootArgs))
	rootCmd.AddCommand(quotaCmd(rootArgs))

	if err := rootCmd.Execute(); err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}
}

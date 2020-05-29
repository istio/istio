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

package cmd

import (
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/pkg/tracing"
	"istio.io/pkg/collateral"
	"istio.io/pkg/version"
)

type rootArgs struct {
	// attributes is the list of name/value pairs of auto-sensed attributes that will be sent with requests
	attributes string

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

	// durationAttributes is the list of name/value pairs of duration attributes that will be sent with requests.
	durationAttributes string

	// bytesAttributes is the list of name/value pairs of bytes attributes that will be sent with requests.
	bytesAttributes string

	// stringMapAttributes is the list of string maps that will be sent with requests
	stringMapAttributes string

	// mixerAddress is the full address (including port) of a mixer instance to call.
	mixerAddress string

	// number of times to repeat the operation
	repeat int

	// number of workers to send requests concurrently.
	concurrency int

	// rate limit of requests per second for each worker.
	rate int

	// whether to print mixer's response. It is useful when using mixc to generate heavy load.
	printResponse bool

	// Maximum number of report instances to include in each report API call.
	reportBatchSize int

	tracingOptions *tracing.Options
}

func addAttributeFlags(cmd *cobra.Command, rootArgs *rootArgs) {
	cmd.PersistentFlags().StringVarP(&rootArgs.mixerAddress, "mixer", "m", "localhost:9091",
		"Address and port of a running Mixer instance")
	cmd.PersistentFlags().IntVarP(&rootArgs.repeat, "repeat", "r", 1,
		"Sends the specified number of requests in quick succession")
	cmd.PersistentFlags().IntVarP(&rootArgs.concurrency, "concurrency", "c", 1,
		"Controls the number of workers used to send requests to Mixer concurrently.")
	cmd.PersistentFlags().IntVarP(&rootArgs.rate, "rate", "", -1,
		"Maximum number of requests per second sent by each worker.")
	cmd.PersistentFlags().BoolVarP(&rootArgs.printResponse, "print_response", "", true,
		"Whether to print mixer's response, useful when generating heavy load with mixc.")
	cmd.PersistentFlags().IntVarP(&rootArgs.reportBatchSize, "report_batch_size", "", 1,
		"Maximum number of report instances to include in each report API call.")

	cmd.PersistentFlags().StringVarP(&rootArgs.attributes, "attributes", "a", "",
		"List of name/value auto-sensed attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.stringAttributes, "string_attributes", "s", "",
		"List of name/value string attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.int64Attributes, "int64_attributes", "i", "",
		"List of name/value int64 attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.doubleAttributes, "double_attributes", "d", "",
		"List of name/value float64 attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.boolAttributes, "bool_attributes", "b", "",
		"List of name/value bool attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.timestampAttributes, "timestamp_attributes", "t", "",
		"List of name/value timestamp attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.durationAttributes, "duration_attributes", "", "",
		"List of name/value duration attributes specified as name1=value1,name2=value2,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.bytesAttributes, "bytes_attributes", "", "",
		"List of name/value bytes attributes specified as name1=b0:b1:b3,name2=b4:b5:b6,...")
	cmd.PersistentFlags().StringVarP(&rootArgs.stringMapAttributes, "stringmap_attributes", "", "",
		"List of name/value string map attributes specified as name1=k1:v1;k2:v2,name2=k3:v3...")

}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, printf, fatalf shared.FormatFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "mixc",
		Short: "Utility to trigger direct calls to Mixer's API.",
		Long: "This command lets you interact with a running instance of\n" +
			"Mixer. Note that you need a pretty good understanding of Mixer's\n" +
			"API in order to use this command.",

		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootArgs := &rootArgs{
		tracingOptions: tracing.DefaultOptions(),
	}

	cc := checkCmd(rootArgs, printf, fatalf)
	rc := reportCmd(rootArgs, printf, fatalf)

	addAttributeFlags(cc, rootArgs)
	addAttributeFlags(rc, rootArgs)

	rootArgs.tracingOptions.AttachCobraFlags(cc)
	rootArgs.tracingOptions.AttachCobraFlags(rc)

	rootCmd.AddCommand(cc)
	rootCmd.AddCommand(rc)
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Mixer Client",
		Section: "mixc CLI",
		Manual:  "Istio Mixer Client",
	}))

	return rootCmd
}

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

package cmd

import (
	"flag"

	"github.com/spf13/cobra"
)

// InitializeFlags carries over glog flags with new defaults, and makes
// `flag.Parsed()` to return true.
func InitializeFlags(rootCmd *cobra.Command) {
	flag.CommandLine.VisitAll(func(gf *flag.Flag) {
		rootCmd.PersistentFlags().AddGoFlag(gf)
	})

	// hack to make flag.Parsed return true such that glog is happy
	// about the flags having been parsed
	flagSet := flag.NewFlagSet("", flag.ContinueOnError)
	_ = flagSet.Parse([]string{})
	flag.CommandLine = flagSet
}

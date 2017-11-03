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
	"github.com/spf13/pflag"
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

// FilterFlags removes unknown flags that is not in the flagset
// from a list of cmd arguments.
func FilterFlags(flags *pflag.FlagSet, args []string) []string {
	if flags == nil || len(args) == 0 {
		return args
	}

	names := make(map[string]bool)
	flags.VisitAll(func(f *pflag.Flag) {
		names[f.Name] = true
	})

	ret := make([]string, 0, len(args))
	for len(args) > 0 {
		s := args[0]
		args = args[1:]
		if len(s) == 0 || len(s) == 1 || s[0] != '-' {
			ret = append(ret, s)
			continue
		}

		var name string
		if s[1] == '-' {
			if len(s) == 2 { // '--' terminates flags
				ret = append(ret, s)
				ret = append(ret, args...)
				break
			}
			name = s[2:]
		} else {
			name = s[1:]
		}

		if !names[name] && name != "h" && name != "help" { // flag is not defined in FlagSet, and is not help
			if len(args) == 0 {
				break
			}

			if len(args[0]) > 0 && args[0][0] != '-' { // unknown flag has an argument
				args = args[1:]
				continue
			}
		} else {
			ret = append(ret, s)
		}
	}
	return ret
}

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

package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// Attributes is the list of name/value pairs of attributes that will be sent with requests.
var Attributes string

// MixerAddress is the full address (including port) of a mixer instance to call.
var MixerAddress string

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "client",
	Short: "Invoke the API of a running instance of the Istio mixer",
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&MixerAddress, "mixer", "m", "localhost:9091", "Address and port of running instance of the mixer")
	RootCmd.PersistentFlags().StringVarP(&Attributes, "attributes", "a", "", "List of name/value attributes specified as name1=value1,name2=value2,...")
}

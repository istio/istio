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

package viperconfig

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/viper"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// AddConfigFlag appends a persistent flag for retrieving Viper config, as well as an initializer
// for reading that file
func AddConfigFlag(rootCmd *cobra.Command, viper *viper.Viper) {
	var cfgFile string
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "Config file containing args")

	cobra.OnInitialize(func() {
		if len(cfgFile) > 0 {
			viper.SetConfigFile(cfgFile)
			err := viper.ReadInConfig() // Find and read the config file
			if err != nil {             // Handle errors reading the config file
				_, _ = os.Stderr.WriteString(fmt.Errorf("fatal error in config file: %s", err).Error())
				os.Exit(1)
			}
		}
	})
}

// ProcessViperConfig retrieves Viper values for each Cobra Val Flag
func ProcessViperConfig(cmd *cobra.Command, viper *viper.Viper) {
	viper.SetTypeByDefaultValue(true)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		k := reflect.TypeOf(viper.Get(f.Name)).Kind()
		if k == reflect.Slice || k == reflect.Array {
			// Viper cannot convert slices to strings, so this is our workaround.
			_ = f.Value.Set(strings.Join(viper.GetStringSlice(f.Name), ","))
		} else {
			_ = f.Value.Set(viper.GetString(f.Name))
		}
	})
}

// ViperizeRootCmd takes a root command, and ensures that all flags of all sub commands
// are bound to viper, with one master config flag accepting a config file.
// At runtime, it will then assign all cobra variables to have their viper values.
func ViperizeRootCmd(cmd *cobra.Command, viper *viper.Viper) {
	AddConfigFlag(cmd, viper)
	subCommandPreRun(cmd, viper)
}

func subCommandPreRun(cmd *cobra.Command, viper *viper.Viper) {
	original := cmd.PreRun
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		if original != nil {
			original(cmd, args)
		}
		_ = viper.BindPFlags(cmd.Flags())
		ProcessViperConfig(cmd, viper)
	}

	for _, c := range cmd.Commands() {
		subCommandPreRun(c, viper)
	}
}

//ViperizeRootCmdDefault calls ViperizeRootCmd using viper.GetViper()
func ViperizeRootCmdDefault(cmd *cobra.Command) {
	ViperizeRootCmd(cmd, viper.GetViper())
}

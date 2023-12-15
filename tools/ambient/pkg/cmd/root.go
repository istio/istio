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
	"github.com/spf13/cobra"

	"istio.io/istio/pkg/flag"
	"istio.io/istio/tools/ambient/pkg/validation"
)

const (
	ValidateAmbient = "validate-ambient"
)

// Command line options
type Config struct {
	ValidateAmbient bool `json:"VALIDATE_AMBIENT"`
}

func DefaultConfig() *Config {
	return &Config{
		ValidateAmbient: true,
	}
}

func bindCmdlineFlags(cfg *Config, cmd *cobra.Command) {
	fs := cmd.Flags()
	flag.BindEnv(fs, ValidateAmbient, "", "Validate ambient proxy is attached.", &cfg.ValidateAmbient)
}

func GetCommand() *cobra.Command {
	cfg := DefaultConfig()
	cmd := &cobra.Command{
		Use:   "istio-ambient",
		Short: "Validate ambient for pods",
		Long:  "istio-ambient is responsible for validating that the ztunnel is attached to the pod.",
		Run: func(cmd *cobra.Command, args []string) {
			if cfg.ValidateAmbient {
				validation.RunAmbientCheck()
			}
		},
	}
	bindCmdlineFlags(cfg, cmd)
	return cmd
}

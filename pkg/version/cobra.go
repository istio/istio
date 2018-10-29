// Copyright 2018 Istio Authors
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

package version

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
)

// Version holds info for client and control plane versions
type Version struct {
	ClientVersion *BuildInfo `json:"clientVersion,omitempty" yaml:"clientVersion,omitempty"`
	MeshVersion   *MeshInfo  `json:"meshVersion,omitempty" yaml:"meshVersion,omitempty"`
}

// GetRemoteVersionFunc is the function protoype to be passed to CobraOptions so that it is
// called when invoking `cmd version`
type GetRemoteVersionFunc func() (*MeshInfo, error)

// CobraOptions holds options to be passed to `CobraCommandWithOptions`
type CobraOptions struct {
	// GetRemoteVersion is the function to be invoked to retrieve remote versions for
	// Istio components. Optional. If not set, the 'version' subcommand will not attempt
	// to connect to a remote side, and CLI flags such as '--remote' will be hidden.
	GetRemoteVersion GetRemoteVersionFunc
}

// CobraCommand returns a command used to print version information.
func CobraCommand() *cobra.Command {
	return CobraCommandWithOptions(CobraOptions{})
}

// CobraCommandWithOptions returns a command used to print version information.
// It accepts an CobraOptions argument that might modify its behavior
func CobraCommandWithOptions(options CobraOptions) *cobra.Command {
	var (
		short         bool
		output        string
		remote        bool
		version       Version
		remoteVersion *MeshInfo
		serverErr     error
	)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Prints out build version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "" && output != "yaml" && output != "json" {
				return errors.New(`--output must be 'yaml' or 'json'`)
			}

			version.ClientVersion = &Info

			if options.GetRemoteVersion != nil && remote {
				remoteVersion, serverErr = options.GetRemoteVersion()
				version.MeshVersion = remoteVersion
			}

			switch output {
			case "":
				if short {
					if remoteVersion != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "client version: %s\n", version.ClientVersion.Version)
						for _, remote := range *remoteVersion {
							fmt.Fprintf(cmd.OutOrStdout(), "%s version: %s\n", remote.Component, remote.Info.Version)
						}

					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s\n", version.ClientVersion.Version)
					}
				} else {
					if remoteVersion != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "client version: %s\n", version.ClientVersion.LongForm())
						for _, remote := range *remoteVersion {
							fmt.Fprintf(cmd.OutOrStdout(), "%s version: %s\n", remote.Component, remote.Info.LongForm())
						}
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s\n", version.ClientVersion.LongForm())
					}
				}
			case "yaml":
				marshalled, _ := yaml.Marshal(&version)
				fmt.Fprintln(cmd.OutOrStdout(), string(marshalled))
			case "json":
				marshalled, _ := json.MarshalIndent(&version, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(marshalled))
			}

			return serverErr
		},
	}

	cmd.Flags().BoolVarP(&short, "short", "s", false, "Displays a short form of the version information")
	cmd.Flags().StringVarP(&output, "output", "o", "", "One of 'yaml' or 'json'.")
	if options.GetRemoteVersion != nil {
		cmd.Flags().BoolVar(&remote, "remote", false, "Prints remote version information, from the control plane")
	}

	return cmd
}

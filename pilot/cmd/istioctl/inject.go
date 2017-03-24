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

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"istio.io/manager/cmd"
	"istio.io/manager/cmd/version"
	"istio.io/manager/platform/kube/inject"

	"github.com/spf13/cobra"
)

var (
	hub              string
	tag              string
	managerAddr      string
	mixerAddr        string
	sidecarProxyUID  int64
	sidecarProxyPort int
	runtimeVerbosity int
	versionStr       string // override build version

	inFilename  string
	outFilename string
)

var (
	injectCmd = &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject istio runtime into kubernete resources",
		RunE: func(_ *cobra.Command, _ []string) (err error) {
			if inFilename == "" {
				return errors.New("filename not specified (see --filename or -f)")
			}
			var reader io.Reader
			if inFilename == "-" {
				reader = os.Stdin
			} else {
				reader, err = os.Open(inFilename)
				if err != nil {
					return err
				}
			}

			var writer io.Writer
			if outFilename == "" {
				writer = os.Stdout
			} else {
				file, err := os.Create(outFilename)
				if err != nil {
					return err
				}
				writer = file
				defer func() { err = file.Close() }()
			}

			if versionStr == "" {
				versionStr = fmt.Sprintf("%v@%v-%v-%v",
					version.Info.User,
					version.Info.Host,
					version.Info.Version,
					version.Info.GitRevision)
			}
			params := &inject.Params{
				InitImage:        inject.InitImageName(hub, tag),
				RuntimeImage:     inject.RuntimeImageName(hub, tag),
				RuntimeVerbosity: runtimeVerbosity,
				ManagerAddr:      managerAddr,
				MixerAddr:        mixerAddr,
				SidecarProxyUID:  sidecarProxyUID,
				SidecarProxyPort: sidecarProxyPort,
				Version:          versionStr,
			}
			return inject.IntoResourceFile(params, reader, writer)
		},
	}
)

func init() {
	injectCmd.PersistentFlags().StringVar(&hub, "hub",
		inject.DefaultHub, "Docker hub")
	injectCmd.PersistentFlags().StringVar(&tag, "tag",
		inject.DefaultTag, "Docker tag")
	injectCmd.PersistentFlags().StringVarP(&inFilename, "filename", "f",
		"", "Input kubernetes resource filename")
	injectCmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"", "Modified output kubernetes resource filename")
	injectCmd.PersistentFlags().StringVar(&managerAddr, "managerAddr",
		inject.DefaultManagerAddr, "Manager service DNS address")
	injectCmd.PersistentFlags().StringVar(&mixerAddr, "mixerAddr",
		inject.DefaultMixerAddr, "Mixer DNS address")
	injectCmd.PersistentFlags().IntVar(&runtimeVerbosity, "verbosity",
		inject.DefaultRuntimeVerbosity, "Runtime verbosity")
	injectCmd.PersistentFlags().Int64Var(&sidecarProxyUID, "sidecarProxyUID",
		inject.DefaultSidecarProxyUID, "Sidecar proxy UID")
	injectCmd.PersistentFlags().IntVar(&sidecarProxyPort, "sidecarProxyPort",
		inject.DefaultSidecarProxyPort, "Sidecar proxy Port")
	injectCmd.PersistentFlags().StringVar(&versionStr, "setVersionString",
		"", "Override version info injected into resource")
	cmd.RootCmd.AddCommand(injectCmd)
}

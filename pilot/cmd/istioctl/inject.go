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
	verbosity        int
	versionStr       string // override build version
	enableCoreDump   bool

	inFilename  string
	outFilename string

	authConfigPath string
	enableAuth     bool
)

var (
	injectCmd = &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject istio sidecar proxy into kubernetes resources",
		Long: `
Use kube-inject to manually inject istio sidecar proxy into kubernetes
resource files. Unsupported resources are left unmodified so it is
safe to run kube-inject over a single file that contains multiple
Service, ConfigMap, Deployment, etc. definitions for a complex
application. Its best to do this when the resource is initially
created.

Example usage:

	kubectl apply -f <(istioctl kube-inject -f <resource.yaml>)
`,
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
				ProxyImage:       inject.ProxyImageName(hub, tag),
				Verbosity:        verbosity,
				ManagerAddr:      managerAddr,
				MixerAddr:        mixerAddr,
				SidecarProxyUID:  sidecarProxyUID,
				SidecarProxyPort: sidecarProxyPort,
				Version:          versionStr,
				EnableCoreDump:   enableCoreDump,
				EnableAuth:       enableAuth,
				AuthConfigPath:   authConfigPath,
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
	injectCmd.PersistentFlags().IntVar(&verbosity, "verbosity",
		inject.DefaultVerbosity, "Runtime verbosity")
	injectCmd.PersistentFlags().Int64Var(&sidecarProxyUID, "sidecarProxyUID",
		inject.DefaultSidecarProxyUID, "Sidecar proxy UID")
	injectCmd.PersistentFlags().IntVar(&sidecarProxyPort, "sidecarProxyPort",
		inject.DefaultSidecarProxyPort, "Sidecar proxy Port")
	injectCmd.PersistentFlags().StringVar(&versionStr, "setVersionString",
		"", "Override version info injected into resource")

	// Default --coreDump=true for pre-alpha development. Core dump
	// settings (i.e. sysctl kernel.*) affect all pods in a node and
	// require privileges. This option should only be used by the cluster
	// admin (see https://kubernetes.io/docs/concepts/cluster-administration/sysctl-cluster/)
	injectCmd.PersistentFlags().BoolVar(&enableCoreDump, "coreDump",
		true, "Enable/Disable core dumps in injected proxy (--coreDump=true affects "+
			"all pods in a node and should only be used the cluster admin)")

	injectCmd.PersistentFlags().BoolVar(&enableAuth, "enable_auth", false,
		"Enable/Disable mutual TLS authentication for proxy-to-proxy traffic")
	injectCmd.PersistentFlags().StringVar(&authConfigPath, "auth_config_path", "/etc/certs/",
		"The directory in which certificate and key files are stored")
}

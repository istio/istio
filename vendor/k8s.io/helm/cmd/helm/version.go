/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/helm/pkg/helm"
	pb "k8s.io/helm/pkg/proto/hapi/version"
	"k8s.io/helm/pkg/version"
)

const versionDesc = `
Show the client and server versions for Helm and tiller.

This will print a representation of the client and server versions of Helm and
Tiller. The output will look something like this:

Client: &version.Version{SemVer:"v2.0.0", GitCommit:"ff52399e51bb880526e9cd0ed8386f6433b74da1", GitTreeState:"clean"}
Server: &version.Version{SemVer:"v2.0.0", GitCommit:"b0c113dfb9f612a9add796549da66c0d294508a3", GitTreeState:"clean"}

- SemVer is the semantic version of the release.
- GitCommit is the SHA for the commit that this version was built from.
- GitTreeState is "clean" if there are no local code changes when this binary was
  built, and "dirty" if the binary was built from locally modified code.

To print just the client version, use '--client'. To print just the server version,
use '--server'.
`

type versionCmd struct {
	out        io.Writer
	client     helm.Interface
	showClient bool
	showServer bool
	short      bool
	template   string
}

func newVersionCmd(c helm.Interface, out io.Writer) *cobra.Command {
	version := &versionCmd{
		client: c,
		out:    out,
	}

	cmd := &cobra.Command{
		Use:   "version",
		Short: "print the client/server version information",
		Long:  versionDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If neither is explicitly set, show both.
			if !version.showClient && !version.showServer {
				version.showClient, version.showServer = true, true
			}
			return version.run()
		},
	}
	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.BoolVarP(&version.showClient, "client", "c", false, "client version only")
	f.BoolVarP(&version.showServer, "server", "s", false, "server version only")
	f.BoolVar(&version.short, "short", false, "print the version number")
	f.StringVar(&version.template, "template", "", "template for version string format")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (v *versionCmd) run() error {
	// Store map data for template rendering
	data := map[string]interface{}{}

	if v.showClient {
		cv := version.GetVersionProto()
		if v.template != "" {
			data["Client"] = cv
		} else {
			fmt.Fprintf(v.out, "Client: %s\n", formatVersion(cv, v.short))
		}
	}

	if !v.showServer {
		return tpl(v.template, data, v.out)
	}

	// We do this manually instead of in PreRun because we only
	// need a tunnel if server version is requested.
	if err := setupConnection(); err != nil {
		return err
	}
	v.client = ensureHelmClient(v.client)

	if settings.Debug {
		k8sVersion, err := getK8sVersion()
		if err != nil {
			return err
		}
		fmt.Fprintf(v.out, "Kubernetes: %#v\n", k8sVersion)
	}
	resp, err := v.client.GetVersion()
	if err != nil {
		if grpc.Code(err) == codes.Unimplemented {
			return errors.New("server is too old to know its version")
		}
		debug("%s", err)
		return errors.New("cannot connect to Tiller")
	}

	if v.template != "" {
		data["Server"] = resp.Version
	} else {
		fmt.Fprintf(v.out, "Server: %s\n", formatVersion(resp.Version, v.short))
	}
	return tpl(v.template, data, v.out)
}

func getK8sVersion() (*apiVersion.Info, error) {
	var v *apiVersion.Info
	_, client, err := getKubeClient(settings.KubeContext, settings.KubeConfig)
	if err != nil {
		return v, err
	}
	v, err = client.Discovery().ServerVersion()
	return v, err
}

func formatVersion(v *pb.Version, short bool) string {
	if short && v.GitCommit != "" {
		return fmt.Sprintf("%s+g%s", v.SemVer, v.GitCommit[:7])
	}
	return fmt.Sprintf("%#v", v)
}

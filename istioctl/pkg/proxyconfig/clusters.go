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

package proxyconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pkg/cluster"
)

// TODO move to multicluster package; requires exposing some private funcs/vars in this package
func ClustersCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "remote-clusters",
		Short: "Lists the remote clusters each istiod instance is connected to.",
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClientWithRevision(ctx.RevisionOrDefault(opts.Revision))
			if err != nil {
				return err
			}
			res, err := kubeClient.AllDiscoveryDo(context.Background(), ctx.IstioNamespace(), "debug/clusterz")
			if err != nil {
				return err
			}
			return writeMulticlusterStatus(cmd.OutOrStdout(), res)
		},
	}
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func writeMulticlusterStatus(out io.Writer, input map[string][]byte) error {
	statuses, err := parseClusterStatuses(input)
	if err != nil {
		return err
	}
	w := new(tabwriter.Writer).Init(out, 0, 8, 5, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSECRET\tSTATUS\tISTIOD")
	for istiod, clusters := range statuses {
		for _, c := range clusters {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ID, c.SecretName, c.SyncStatus, istiod)
		}
	}
	_ = w.Flush()
	return nil
}

func parseClusterStatuses(input map[string][]byte) (map[string][]cluster.DebugInfo, error) {
	statuses := make(map[string][]cluster.DebugInfo, len(input))
	for istiodKey, bytes := range input {
		var parsed []cluster.DebugInfo
		if err := json.Unmarshal(bytes, &parsed); err != nil {
			return nil, err
		}
		statuses[istiodKey] = parsed
	}
	return statuses, nil
}

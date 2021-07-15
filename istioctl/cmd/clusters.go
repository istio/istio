package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"istio.io/istio/pkg/cluster"
	"text/tabwriter"

	"istio.io/istio/istioctl/pkg/clioptions"
)

// TODO move to multicluster package; requires exposing some private funcs/vars in this package
func clustersCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "remote-clusters",
		Short: "Lists the remote clusters each istiod instance is connected to.",
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}
			res, err := kubeClient.AllDiscoveryDo(context.Background(), istioNamespace, "/debug/clusterz")
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

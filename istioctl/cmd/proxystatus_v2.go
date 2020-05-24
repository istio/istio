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
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corev2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/ptypes"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
)

const (
	connectionsTypeURL = "istio.io/connections"
)

func newProxyStatusCommand() *cobra.Command {
	var opts clioptions.CentralControlPlaneOptions

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<pod-name[.namespace]>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Pilot to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync diff for a single Envoy and Pilot
	istioctl proxy-status productpage-v1-679f4bcbbb-6j4j6.default
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			if opts.Xds == "" {
				return fmt.Errorf("--endpoint default not implemented; MUST be supplied; try localhost:15012")
			}
			adscConn, err := adsc.Dial(opts.Xds, "", &adsc.Config{
				IP: "1.2.3.4",
				Meta: model.NodeMetadata{
					Generator: "event",
				}.ToStruct(),
				// TODO I tried and failed to add DialOptions here and have ADSC
				// pass them down so that I could add gRPC Interceptors for logging
				// and debugging.
			})
			if err != nil {
				return fmt.Errorf("Could not dial: %w", err)
			}
			if len(args) > 0 {
				return fmt.Errorf("experimental proxy-status <pod> unimplemented")
			}

			err = adscConn.Send(&xdsapi.DiscoveryRequest{
				Node: &corev2.Node{
					Id: "sidecar~1.1.1.1~debug~cluster.local",
				},
				TypeUrl: connectionsTypeURL,
			})
			if err != nil {
				return err
			}

			// TODO --timeout parameter
			dr, err := adscConn.WaitVersion(10*time.Second, connectionsTypeURL, "")
			if err != nil {
				return err
			}

			nodes := make([]corev2.Node, 0)
			for _, resource := range dr.Resources {
				switch resource.TypeUrl {
				case "type.googleapis.com/envoy.api.v2.core.Node":
					node := corev2.Node{}
					err = ptypes.UnmarshalAny(resource, &node)
					if err != nil {
						return fmt.Errorf("Could not unmarshal Node: %w", err)
					}
					nodes = append(nodes, node)
				default:
					fmt.Fprintf(os.Stderr, "unexpected resource type %q\n", resource.TypeUrl)
				}
			}

			err = printNodeConnections(nodes, c.OutOrStdout())
			return err
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)

	return statusCmd
}

func printNodeConnections(nodes []corev2.Node, writer io.Writer) error {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tMESH\tCDS\tLDS\tEDS\tRDS\tPILOT")

	for _, node := range nodes {
		/*
			// This isn't working... everything ends up in meta.Raw...
			// Is there a better way to turn a *structpb.Struct into a model.Metadata?
			// Should I even be trying to get a model.Metadata instead of working with the Struct?
			b, err := json.Marshal(node.GetMetadata())
			if err != nil {
				return err
			}
			meta := &model.NodeMetadata{}
			if err := meta.UnmarshalJSON(b); err != nil {
				return err
			}
			fmt.Printf("Metadata: %v#\n", meta)
			fmt.Printf("Metadata.ProxyConfig: %v#\n", meta.ProxyConfig)
			fmt.Printf("Metadata.Raw: %v#\n", meta.Raw)
			fmt.Printf("%s.%s\t%s\n", meta.InstanceName, meta.Namespace, meta.MeshID)
		*/
		fields := node.GetMetadata().GetFields()
		name := fields["NAME"]
		if name.GetStringValue() == "" {
			// Skip the unnamed node, which represents the XDS request node?!?
			continue
		}
		ns := fields["NAMESPACE"]
		meshID := fields["MESH_ID"]
		fmt.Fprintf(w, "%s.%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			name.GetStringValue(), ns.GetStringValue(), meshID.GetStringValue(),
			"TODO", "TODO", "TODO", "TODO", "TODO")
	}
	return w.Flush()
}

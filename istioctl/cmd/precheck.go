// Copyright © 2021 NAME HERE <EMAIL ADDRESS>
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
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
)

func preCheck() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var namespaces []string
	var skipControlPlane bool
	// cmd represents the upgradeCheck command
	cmd := &cobra.Command{
		Use:     "pre-check",
		Short:   "check whether Istio can safely be installed or upgrade",
		Long:    `precheck inspects a Kubernetes cluster for Istio install and upgrade requirements.`,
		Example: `TODO`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			cli, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
			if err != nil {
				return err
			}

			msgs := diag.Messages{}
			if !skipControlPlane {
				msgs, err = checkControlPlane(cli)
				if err != nil {
					return err
				}
			}
			if len(namespaces) == 0 {
				namespaces = []string{metav1.NamespaceAll}
			}
			for _, ns := range namespaces {
				nsmsgs, err := checkDataPlane(cli, ns)
				if err != nil {
					return err
				}
				msgs.Add(nsmsgs...)

			}
			// Print all the messages to stdout in the specified format
			msgs = msgs.SortedDedupedCopy()
			output, err := formatting.Print(msgs.SortedDedupedCopy(), msgOutputFormat, colorize)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), output)
			if len(msgs) > 0 {
				os.Exit(2)
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringArrayVar(&namespaces, "namespaces", nil, "check the dataplane in these specific namespaces. If not provided, all namespaces are checked")
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func checkControlPlane(cli kube.ExtendedClient) (diag.Messages, error) {
	// TODO add control plane analyzers
	sa := local.NewSourceAnalyzer(schema.MustGet(), analysis.Combine("upgrade precheck"),
		resource.Namespace(metav1.NamespaceAll), resource.Namespace(istioNamespace), nil, true, analysisTimeout)
	sa.AddRunningKubeSource(cfgKube.NewInterfaces(cli.RESTConfig()))

	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func checkDataPlane(cli kube.ExtendedClient, namespace string) (diag.Messages, error) {
	msgs := diag.Messages{}

	listenerMsgs, err := checkListeners(cli, namespace)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, listenerMsgs...)

	// TODO: add more checks

	return msgs, nil
}

func checkListeners(cli kube.ExtendedClient, n string) (diag.Messages, error) {
	// TODO real message
	mt := diag.NewMessageType(diag.Warning, "IST1337", "Port %v listens on localhost and will no longer be exposed to other pods.")

	pods, err := cli.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		// Find all injected pods
		LabelSelector: "security.istio.io/tlsMode=istio",
	})
	if err != nil {
		return nil, err
	}

	var messages diag.Messages = make([]diag.Message, 0)
	g := errgroup.Group{}

	sem := semaphore.NewWeighted(25)
	for _, pod := range pods.Items {
		pod := pod
		g.Go(func() error {
			sem.Acquire(context.Background(), 1)
			defer sem.Release(1)
			// Fetch list of all clusters to get which ports we care about
			resp, err := cli.EnvoyDo(context.Background(), pod.Name, pod.Namespace,
				"GET", "config_dump?resource=dynamic_active_clusters&mask=cluster.name", nil)
			if err != nil {
				fmt.Println("failed to get config dump: ", err)
				return nil
			}
			ports, err := extractInboundPorts(resp)
			if err != nil {
				fmt.Println("failed to get ports: ", err)
				return nil
			}

			// Next, look at what ports the pod is actually listening on
			// This requires parsing the output from ss; the version we use doesn't support JSON
			out, _, err := cli.PodExec(pod.Name, pod.Namespace, "istio-proxy", "ss -ltnH")
			if err != nil {
				if strings.Contains(err.Error(), "executable file not found") {
					// Likely distroless or other custom build without ss. Nothing we can do here...
					return nil
				}
				fmt.Println("failed to get listener state: ", err)
				return nil
			}
			for _, ss := range strings.Split(out, "\n") {
				if len(ss) == 0 {
					continue
				}
				bind, port, err := net.SplitHostPort(getColumn(ss, 3))
				if err != nil {
					fmt.Println("failed to get parse state: ", err)
					continue
				}
				ip := net.ParseIP(bind)
				ip.IsGlobalUnicast()
				portn, _ := strconv.Atoi(port)
				if _, f := ports[portn]; f {
					c := ports[portn]
					if bind == "" {
						continue
					} else if bind == "*" || ip.IsUnspecified() {
						c.Wildcard = true
					} else if ip.IsLoopback() {
						c.Lo = true
					} else {
						c.Explicit = true
					}
					ports[portn] = c
				}
			}

			origin := &rt.Origin{
				Collection: collections.K8SCoreV1Pods.Name(),
				Kind:       collections.K8SCoreV1Pods.Resource().Kind(),
				FullName: resource.FullName{
					Namespace: resource.Namespace(pod.Namespace),
					Name:      resource.LocalName(pod.Name),
				},
				Version: resource.Version(pod.ResourceVersion),
			}
			for port, status := range ports {
				// Binding to localhost no longer works out of the box on Istio 1.10+, give them a warning.
				if status.Lo == true {
					messages.Add(diag.NewMessage(mt, &resource.Instance{Origin: origin}, fmt.Sprint(port)))
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return messages, nil
}

func getColumn(line string, col int) string {
	res := []byte{}
	prevSpace := false
	for _, c := range line {
		if col < 0 {
			return string(res)
		}
		if c == ' ' {
			if !prevSpace {
				col--
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		if col == 0 {
			res = append(res, byte(c))
		}
	}
	return string(res)
}

func extractInboundPorts(configdump []byte) (map[int]bindStatus, error) {
	ports := map[int]bindStatus{}
	cd := &adminapi.ConfigDump{}
	if err := jsonpb.Unmarshal(bytes.NewReader(configdump), cd); err != nil {
		return nil, err
	}
	for _, cdump := range cd.Configs {
		clw := &adminapi.ClustersConfigDump_DynamicCluster{}
		if err := ptypes.UnmarshalAny(cdump, clw); err != nil {
			return nil, err
		}
		cl := &cluster.Cluster{}
		if err := ptypes.UnmarshalAny(clw.Cluster, cl); err != nil {
			return nil, err
		}
		dir, _, _, port := model.ParseSubsetKey(cl.Name)
		if dir == model.TrafficDirectionInbound {
			ports[port] = bindStatus{}
		}
	}
	return ports, nil
}

type bindStatus struct {
	Lo       bool
	Wildcard bool
	Explicit bool
}

func (b bindStatus) Any() bool {
	return b.Lo || b.Wildcard || b.Explicit
}

func (b bindStatus) String() string {
	res := []string{}
	if b.Lo {
		res = append(res, "Localhost")
	}
	if b.Wildcard {
		res = append(res, "Wildcard")
	}
	if b.Explicit {
		res = append(res, "Explicit")
	}
	if len(res) == 0 {
		return "Unknown"
	}
	return strings.Join(res, ", ")
}

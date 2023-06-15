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

package precheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/fatih/color"
	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	authorizationapi "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	crd "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/istioctl/pkg/util/formatting"
	pkgversion "istio.io/istio/operator/pkg/version"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	kube3 "istio.io/istio/pkg/config/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/url"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

var revision string

func Cmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var skipControlPlane bool
	// cmd represents the upgradeCheck command
	cmd := &cobra.Command{
		Use:   "precheck",
		Short: "Check whether Istio can safely be installed or upgrade",
		Long:  `precheck inspects a Kubernetes cluster for Istio install and upgrade requirements.`,
		Example: `  # Verify that Istio can be installed or upgraded
  istioctl x precheck

  # Check only a single namespace
  istioctl x precheck --namespace default`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			cli, err := ctx.CLIClientWithRevision(revision)
			if err != nil {
				return err
			}

			msgs := diag.Messages{}
			if !skipControlPlane {
				msgs, err = checkControlPlane(ctx)
				if err != nil {
					return err
				}
			}
			nsmsgs, err := checkDataPlane(cli, ctx.Namespace())
			if err != nil {
				return err
			}
			msgs.Add(nsmsgs...)
			// Print all the messages to stdout in the specified format
			msgs = msgs.SortedDedupedCopy()
			output, err := formatting.Print(msgs, formatting.LogFormat, false)
			if err != nil {
				return err
			}
			if len(msgs) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), color.New(color.FgGreen).Sprint("✔")+" No issues found when checking the cluster. Istio is safe to install or upgrade!\n"+
					"  To get started, check out https://istio.io/latest/docs/setup/getting-started/\n")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), output)
			}
			for _, m := range msgs {
				if m.Type.Level().IsWorseThanOrEqualTo(diag.Warning) {
					e := fmt.Sprintf(`Issues found when checking the cluster. Istio may not be safe to install or upgrade.
See %s for more information about causes and resolutions.`, url.ConfigAnalysis)
					return errors.New(e)
				}
			}
			return nil
		},
	}
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func checkControlPlane(ctx cli.Context) (diag.Messages, error) {
	cli, err := ctx.CLIClient()
	if err != nil {
		return nil, err
	}
	msgs := diag.Messages{}

	m, err := checkServerVersion(cli)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, m...)

	msgs = append(msgs, checkInstallPermissions(cli, ctx.IstioNamespace())...)
	gwMsg, err := checkGatewayAPIs(cli)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, gwMsg...)

	// TODO: add more checks

	sa := local.NewSourceAnalyzer(
		analysis.Combine("upgrade precheck", &maturity.AlphaAnalyzer{}),
		resource.Namespace(ctx.Namespace()),
		resource.Namespace(ctx.IstioNamespace()),
		nil,
	)
	if err != nil {
		return nil, err
	}
	sa.AddRunningKubeSource(cli)
	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return nil, err
	}
	if result.Messages != nil {
		msgs = append(msgs, result.Messages...)
	}

	return msgs, nil
}

// Checks that if the user has gateway APIs, they are the minimum version.
// It is ok to not have them, but they must be at least v1beta1 if they do.
func checkGatewayAPIs(cli kube.CLIClient) (diag.Messages, error) {
	msgs := diag.Messages{}
	res, err := cli.Ext().ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	betaKinds := sets.New(gvk.KubernetesGateway.Kind, gvk.GatewayClass.Kind, gvk.HTTPRoute.Kind, gvk.ReferenceGrant.Kind)
	for _, r := range res.Items {
		if r.Spec.Group != gvk.KubernetesGateway.Group {
			continue
		}
		if !betaKinds.Contains(r.Spec.Names.Kind) {
			continue
		}

		versions := extractCRDVersions(&r)
		has := "none"
		if len(versions) > 0 {
			has = strings.Join(sets.SortedList(versions), ",")
		}
		if !versions.Contains(gvk.KubernetesGateway.Version) {
			origin := kube3.Origin{
				Type: gvk.CustomResourceDefinition,
				FullName: resource.FullName{
					Namespace: resource.Namespace(r.Namespace),
					Name:      resource.LocalName(r.Name),
				},
				ResourceVersion: resource.Version(r.ResourceVersion),
			}
			r := &resource.Instance{
				Origin: &origin,
			}
			msgs.Add(msg.NewUnsupportedGatewayAPIVersion(r, has, gvk.KubernetesGateway.Version))
		}
	}
	return msgs, nil
}

func extractCRDVersions(r *crd.CustomResourceDefinition) sets.String {
	res := sets.New[string]()
	for _, v := range r.Spec.Versions {
		if v.Served {
			res.Insert(v.Name)
		}
	}
	return res
}

func checkInstallPermissions(cli kube.CLIClient, istioNamespace string) diag.Messages {
	Resources := []struct {
		namespace string
		group     string
		version   string
		name      string
	}{
		{
			version: "v1",
			name:    "Namespace",
		},
		{
			namespace: istioNamespace,
			group:     "rbac.authorization.k8s.io",
			version:   "v1",
			name:      "ClusterRole",
		},
		{
			namespace: istioNamespace,
			group:     "rbac.authorization.k8s.io",
			version:   "v1",
			name:      "ClusterRoleBinding",
		},
		{
			namespace: istioNamespace,
			group:     "apiextensions.k8s.io",
			version:   "v1",
			name:      "CustomResourceDefinition",
		},
		{
			namespace: istioNamespace,
			group:     "rbac.authorization.k8s.io",
			version:   "v1",
			name:      "Role",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			name:      "ServiceAccount",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			name:      "Service",
		},
		{
			namespace: istioNamespace,
			group:     "apps",
			version:   "v1",
			name:      "Deployments",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			name:      "ConfigMap",
		},
		{
			group:   "admissionregistration.k8s.io",
			version: "v1",
			name:    "MutatingWebhookConfiguration",
		},
		{
			group:   "admissionregistration.k8s.io",
			version: "v1",
			name:    "ValidatingWebhookConfiguration",
		},
	}
	msgs := diag.Messages{}
	for _, r := range Resources {
		err := checkCanCreateResources(cli, r.namespace, r.group, r.version, r.name)
		if err != nil {
			msgs.Add(msg.NewInsufficientPermissions(&resource.Instance{Origin: clusterOrigin{}}, r.name, err.Error()))
		}
	}
	return msgs
}

func checkCanCreateResources(c kube.CLIClient, namespace, group, version, name string) error {
	s := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     group,
				Version:   version,
				Resource:  name,
			},
		},
	}

	response, err := c.Kube().AuthorizationV1().SelfSubjectAccessReviews().Create(context.Background(), s, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if len(response.Status.Reason) > 0 {
			return errors.New(response.Status.Reason)
		}
		return errors.New("permission denied")
	}
	return nil
}

func checkServerVersion(cli kube.CLIClient) (diag.Messages, error) {
	v, err := cli.GetKubernetesVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get the Kubernetes version: %v", err)
	}
	compatible, err := k8sversion.CheckKubernetesVersion(v)
	if err != nil {
		return nil, err
	}
	if !compatible {
		return []diag.Message{
			msg.NewUnsupportedKubernetesVersion(&resource.Instance{Origin: clusterOrigin{}}, v.String(), fmt.Sprintf("1.%d", k8sversion.MinK8SVersion)),
		}, nil
	}
	return nil, nil
}

func checkDataPlane(cli kube.CLIClient, namespace string) (diag.Messages, error) {
	msgs := diag.Messages{}

	m, err := checkListeners(cli, namespace)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, m...)

	// TODO: add more checks

	return msgs, nil
}

var networkingChanges, _ = goversion.NewSemver("1.10.0")

func fromLegacyNetworkingVersion(pod v1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name != "istio-proxy" {
			continue
		}
		_, tag, _ := strings.Cut(c.Image, ":")
		ver, err := pkgversion.TagToVersionString(tag)
		if err != nil {
			return true // If we aren't sure, default to doing more checks than needed
		}
		sv, err := goversion.NewSemver(ver)
		if err != nil {
			return true // If we aren't sure, default to doing more checks than needed
		}
		return sv.LessThan(networkingChanges)
	}
	return false
}

// checkListeners checks for workloads that would be broken by https://istio.io/latest/blog/2021/upcoming-networking-changes/
func checkListeners(cli kube.CLIClient, namespace string) (diag.Messages, error) {
	pods, err := cli.Kube().CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		// Find all running pods
		FieldSelector: "status.phase=Running",
		// Find all injected pods. We don't care about non-injected pods, because the new behavior
		// mirrors Kubernetes; this is only a breaking change for existing Istio users.
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
		if !fromLegacyNetworkingVersion(pod) {
			// Skip check. This pod is already on a version where the change has been made; if they were going
			// to break they would already be broken.
			continue
		}
		g.Go(func() error {
			_ = sem.Acquire(context.Background(), 1)
			defer sem.Release(1)
			// Fetch list of all clusters to get which ports we care about
			resp, err := cli.EnvoyDo(context.Background(), pod.Name, pod.Namespace, "GET", "config_dump?resource=dynamic_active_clusters&mask=cluster.name")
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
				ip, _ := netip.ParseAddr(bind)
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

			origin := &kube3.Origin{
				Type: gvk.Pod,
				FullName: resource.FullName{
					Namespace: resource.Namespace(pod.Namespace),
					Name:      resource.LocalName(pod.Name),
				},
				ResourceVersion: resource.Version(pod.ResourceVersion),
			}
			for port, status := range ports {
				// Binding to localhost no longer works out of the box on Istio 1.10+, give them a warning.
				if status.Lo {
					messages.Add(msg.NewLocalhostListener(&resource.Instance{Origin: origin}, fmt.Sprint(port)))
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
	cd := &admin.ConfigDump{}
	if err := protomarshal.Unmarshal(configdump, cd); err != nil {
		return nil, err
	}
	for _, cdump := range cd.Configs {
		clw := &admin.ClustersConfigDump_DynamicCluster{}
		if err := cdump.UnmarshalTo(clw); err != nil {
			return nil, err
		}
		cl := &cluster.Cluster{}
		if err := clw.Cluster.UnmarshalTo(cl); err != nil {
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

// clusterOrigin defines an Origin that refers to the cluster
type clusterOrigin struct{}

func (o clusterOrigin) String() string {
	return ""
}

func (o clusterOrigin) FriendlyName() string {
	return "Cluster"
}

func (o clusterOrigin) Comparator() string {
	return o.FriendlyName()
}

func (o clusterOrigin) Namespace() resource.Namespace {
	return ""
}

func (o clusterOrigin) Reference() resource.Reference {
	return nil
}

func (o clusterOrigin) FieldMap() map[string]int {
	return make(map[string]int)
}

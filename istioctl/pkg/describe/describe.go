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

package describe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	apiannotation "istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	typev1beta1 "istio.io/api/type/v1beta1"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/completion"
	istioctlutil "istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	istio_envoy_configdump "istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn"
	pilotcontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	analyzerutil "istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/mesh"
	protocolinstance "istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/url"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

type myProtoValue struct {
	*structpb.Value
}

const (
	k8sSuffix = ".svc." + constants.DefaultClusterLocalDomain

	printLevel0 = 0
	printLevel1 = 3
	printLevel2 = 6
)

func printSpaces(numSpaces int) string {
	return strings.Repeat(" ", numSpaces)
}

var (
	// Ignore unmeshed pods.  This makes it easy to suppress warnings about kube-system etc
	ignoreUnmeshed = false

	describeNamespace string
)

func podDescribeCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "pod <pod-name>[.<namespace>]",
		Aliases: []string{"po"},
		Short:   "Describe pods and their Istio configuration [kube-only]",
		Long: `Analyzes pod, its Services, DestinationRules, and VirtualServices and reports
the configuration objects that affect that pod.`,
		Example: `  #Pod query with inferred namespace (current context's namespace)
  istioctl experimental describe pod helloworld-v1-676yyy3y5r-d8hdl

  # Pod query with explicit namespace 
  istioctl experimental describe pod istio-eastwestgateway-7f4b4f44b-6zd95.istio-system
  or
  istioctl experimental describe pod istio-eastwestgateway-7f4b4f44b-6zd95 -n istio-system`,
		RunE: func(cmd *cobra.Command, args []string) error {
			describeNamespace = ctx.NamespaceOrDefault(ctx.Namespace())
			if len(args) != 1 {
				return fmt.Errorf("expecting pod name")
			}

			podName, ns := handlers.InferPodInfo(args[0], describeNamespace)

			client, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			pod, err := client.Kube().CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()

			podLabels := klabels.Set(pod.ObjectMeta.Labels)
			annotations := klabels.Set(pod.ObjectMeta.Annotations)
			opts.Revision = GetRevisionFromPodAnnotation(annotations)

			printPod(writer, pod, opts.Revision)

			svcs, err := client.Kube().CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			matchingServices := make([]corev1.Service, 0, len(svcs.Items))
			for _, svc := range svcs.Items {
				if len(svc.Spec.Selector) > 0 {
					svcSelector := klabels.SelectorFromSet(svc.Spec.Selector)
					if svcSelector.Matches(podLabels) {
						matchingServices = append(matchingServices, svc)
					}
				}
			}
			// Validate Istio's "Service association" requirement
			if len(matchingServices) == 0 && !ignoreUnmeshed {
				fmt.Fprintf(cmd.OutOrStdout(),
					"Warning: No Kubernetes Services select pod %s (see https://istio.io/docs/setup/kubernetes/additional-setup/requirements/ )\n", // nolint: lll
					kname(pod.ObjectMeta))
			}
			// TODO look for port collisions between services targeting this pod

			kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return err
			}

			configClient := client.Istio()

			podsLabels := []klabels.Set{klabels.Set(pod.ObjectMeta.Labels)}
			fmt.Fprintf(writer, "--------------------\n")
			err = describePodServices(writer, kubeClient, configClient, pod, matchingServices, podsLabels)
			if err != nil {
				return err
			}

			// render PeerAuthentication info
			fmt.Fprintf(writer, "--------------------\n")
			err = describePeerAuthentication(writer, kubeClient, configClient, ns, klabels.Set(pod.ObjectMeta.Labels), ctx.IstioNamespace())
			if err != nil {
				return err
			}

			// TODO find sidecar configs that select this workload and render them

			// Now look for ingress gateways
			return printIngressInfo(writer, matchingServices, podsLabels, client.Kube(), configClient, kubeClient)
		},
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	cmd.PersistentFlags().BoolVar(&ignoreUnmeshed, "ignoreUnmeshed", false,
		"Suppress warnings for unmeshed pods")
	cmd.Long += "\n\n" + istioctlutil.ExperimentalMsg
	return cmd
}

func GetRevisionFromPodAnnotation(anno klabels.Set) string {
	if v, ok := anno[label.IoIstioRev.Name]; ok {
		return v
	}
	statusString := anno.Get(apiannotation.SidecarStatus.Name)
	var injectionStatus inject.SidecarInjectionStatus
	if err := json.Unmarshal([]byte(statusString), &injectionStatus); err != nil {
		return ""
	}

	return injectionStatus.Revision
}

func Cmd(ctx cli.Context) *cobra.Command {
	describeCmd := &cobra.Command{
		Use:     "describe",
		Aliases: []string{"des"},
		Short:   "Describe resource and related Istio configuration",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown resource type %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			describeNamespace = ctx.NamespaceOrDefault(ctx.Namespace())
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	describeCmd.AddCommand(podDescribeCmd(ctx))
	describeCmd.AddCommand(svcDescribeCmd(ctx))
	return describeCmd
}

// Append ".svc.cluster.local" if it isn't already present
func extendFQDN(host string) string {
	if host[0] == '*' {
		return host
	}
	if strings.HasSuffix(host, k8sSuffix) {
		return host
	}
	return host + k8sSuffix
}

// getDestRuleSubsets gets names of subsets that match any pod labels (also, ones that don't match).
func getDestRuleSubsets(subsets []*v1alpha3.Subset, podsLabels []klabels.Set) ([]string, []string) {
	matchingSubsets := make([]string, 0, len(subsets))
	nonmatchingSubsets := make([]string, 0, len(subsets))
	for _, subset := range subsets {
		subsetSelector := klabels.SelectorFromSet(subset.Labels)
		if matchesAnyPod(subsetSelector, podsLabels) {
			matchingSubsets = append(matchingSubsets, subset.Name)
		} else {
			nonmatchingSubsets = append(nonmatchingSubsets, subset.Name)
		}
	}

	return matchingSubsets, nonmatchingSubsets
}

func matchesAnyPod(subsetSelector klabels.Selector, podsLabels []klabels.Set) bool {
	for _, podLabels := range podsLabels {
		if subsetSelector.Matches(podLabels) {
			return true
		}
	}
	return false
}

func printDestinationRule(writer io.Writer, initPrintNum int,
	dr *clientnetworking.DestinationRule, podsLabels []klabels.Set,
) {
	fmt.Fprintf(writer, "%sDestinationRule: %s for %q\n",
		printSpaces(initPrintNum+printLevel0), kname(dr.ObjectMeta), dr.Spec.Host)

	matchingSubsets, nonmatchingSubsets := getDestRuleSubsets(dr.Spec.Subsets, podsLabels)
	if len(matchingSubsets) != 0 || len(nonmatchingSubsets) != 0 {
		if len(matchingSubsets) == 0 {
			fmt.Fprintf(writer, "%sWARNING POD DOES NOT MATCH ANY SUBSETS.  (Non matching subsets %s)\n",
				printSpaces(initPrintNum+printLevel1), strings.Join(nonmatchingSubsets, ","))
		}
		fmt.Fprintf(writer, "%sMatching subsets: %s\n",
			printSpaces(initPrintNum+printLevel1), strings.Join(matchingSubsets, ","))
		if len(nonmatchingSubsets) > 0 {
			fmt.Fprintf(writer, "%s(Non-matching subsets %s)\n",
				printSpaces(initPrintNum+printLevel2), strings.Join(nonmatchingSubsets, ","))
		}
	}

	// Ignore LoadBalancer, ConnectionPool, OutlierDetection
	trafficPolicy := dr.Spec.TrafficPolicy
	if trafficPolicy == nil {
		fmt.Fprintf(writer, "%sNo Traffic Policy\n", printSpaces(initPrintNum+printLevel1))
	} else {
		if trafficPolicy.Tls != nil {
			fmt.Fprintf(writer, "%sTraffic Policy TLS Mode: %s\n",
				printSpaces(initPrintNum+printLevel1), dr.Spec.TrafficPolicy.Tls.Mode.String())
		}
		shortPolicies := recordShortPolicies(
			trafficPolicy.LoadBalancer,
			trafficPolicy.ConnectionPool,
			trafficPolicy.OutlierDetection)
		if shortPolicies != "" {
			fmt.Fprintf(writer, "%s%s", printSpaces(initPrintNum+printLevel1), shortPolicies)
		}

		if trafficPolicy.PortLevelSettings != nil {
			fmt.Fprintf(writer, "%sPort Level Settings:\n", printSpaces(initPrintNum+printLevel1))
			for _, ps := range trafficPolicy.PortLevelSettings {
				fmt.Fprintf(writer, "%s%d:\n", printSpaces(4), ps.GetPort().GetNumber())
				if ps.Tls != nil {
					fmt.Fprintf(writer, "%sTLS Mode: %s\n", printSpaces(initPrintNum+printLevel2), ps.Tls.Mode.String())
				}
				if sp := recordShortPolicies(
					ps.LoadBalancer,
					ps.ConnectionPool,
					ps.OutlierDetection); sp != "" {
					fmt.Fprintf(writer, "%s%s", printSpaces(initPrintNum+printLevel2), sp)
				}
			}
		}
	}
}

func recordShortPolicies(lb *v1alpha3.LoadBalancerSettings,
	connectionPool *v1alpha3.ConnectionPoolSettings,
	outlierDetection *v1alpha3.OutlierDetection,
) string {
	extra := make([]string, 0)
	if lb != nil {
		extra = append(extra, "load balancer")
	}
	if connectionPool != nil {
		extra = append(extra, "connection pool")
	}
	if outlierDetection != nil {
		extra = append(extra, "outlier detection")
	}
	if len(extra) > 0 {
		return fmt.Sprintf("Policies: %s\n", strings.Join(extra, "/"))
	}
	return ""
}

// httpRouteMatchSvc returns true if it matches and a slice of facts about the match
func httpRouteMatchSvc(vs *clientnetworking.VirtualService, route *v1alpha3.HTTPRoute, svc corev1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *clientnetworking.DestinationRule) (bool, []string) { // nolint: lll
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	facts := []string{}
	mismatchNotes := []string{}
	match := false
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, config.Meta{Namespace: vs.Namespace}))
		if extendFQDN(fqdn) == svcHost {
			if dest.Destination.Subset != "" {
				if slices.Contains(nonmatchingSubsets, dest.Destination.Subset) {
					mismatchNotes = append(mismatchNotes, fmt.Sprintf("Route to non-matching subset %s for (%s)",
						dest.Destination.Subset,
						renderMatches(route.Match)))
					continue
				}
				if !slices.Contains(matchingSubsets, dest.Destination.Subset) {
					if dr == nil {
						// Don't bother giving the match conditions, the problem is that there are unknowns in the VirtualService
						mismatchNotes = append(mismatchNotes, fmt.Sprintf("Warning: Route to subset %s but NO DESTINATION RULE defining subsets!", dest.Destination.Subset))
					} else {
						// Don't bother giving the match conditions, the problem is that there are unknowns in the VirtualService
						mismatchNotes = append(mismatchNotes,
							fmt.Sprintf("Warning: Route to UNKNOWN subset %s; check DestinationRule %s", dest.Destination.Subset, kname(dr.ObjectMeta)))
					}
					continue
				}
			}

			match = true
			if dest.Weight > 0 {
				fact := fmt.Sprintf("Route to host \"%s\"", dest.Destination.Host)
				if dest.Destination.Subset != "" {
					fact = fmt.Sprintf("%s subset \"%s\"", fact, dest.Destination.Subset)
				}
				fact = fmt.Sprintf("%s with weight %d%%", fact, dest.Weight)
				facts = append(facts, fact)
			}
			// Consider adding RemoveResponseHeaders, AppendResponseHeaders, RemoveRequestHeaders, AppendRequestHeaders
		} else {
			if dest.Destination.Subset == "" {
				differentHostFact := fmt.Sprintf("Route to host \"%s\" with weight %d%%", dest.Destination.Host, dest.Weight)
				facts = append(facts, differentHostFact)
			} else {
				facts = append(facts, fmt.Sprintf("Route to %s with invalid config", dest.Destination.Host))
			}
		}
	}

	if match {
		reqMatchFacts := []string{}

		if route.Fault != nil {
			reqMatchFacts = append(reqMatchFacts, fmt.Sprintf("Fault injection %s", route.Fault.String()))
		}

		// TODO Consider adding Headers, SourceLabels

		for _, trafficMatch := range route.Match {
			reqMatchFacts = append(reqMatchFacts, renderMatch(trafficMatch))
		}

		if len(reqMatchFacts) > 0 {
			facts = append(facts, strings.Join(reqMatchFacts, ", "))
		}
	}

	if !match && len(mismatchNotes) > 0 {
		facts = append(facts, mismatchNotes...)
	}
	return match, facts
}

func tcpRouteMatchSvc(vs *clientnetworking.VirtualService, route *v1alpha3.TCPRoute, svc corev1.Service) (bool, []string) {
	match := false
	facts := []string{}
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, config.Meta{Namespace: vs.Namespace}))
		if extendFQDN(fqdn) == svcHost {
			match = true
		}
	}

	if match {
		for _, trafficMatch := range route.Match {
			facts = append(facts, trafficMatch.String())
		}
	}

	return match, facts
}

func renderStringMatch(sm *v1alpha3.StringMatch) string {
	if sm == nil {
		return ""
	}

	switch x := sm.MatchType.(type) {
	case *v1alpha3.StringMatch_Exact:
		return x.Exact
	case *v1alpha3.StringMatch_Prefix:
		return x.Prefix + "*"
	}

	return sm.String()
}

func renderMatches(trafficMatches []*v1alpha3.HTTPMatchRequest) string {
	if len(trafficMatches) == 0 {
		return "everything"
	}

	matches := []string{}
	for _, trafficMatch := range trafficMatches {
		matches = append(matches, renderMatch(trafficMatch))
	}
	return strings.Join(matches, ", ")
}

func renderMatch(match *v1alpha3.HTTPMatchRequest) string {
	retval := "Match: "
	// TODO Are users interested in seeing Scheme, Method, Authority?
	if match.Uri != nil {
		retval += renderStringMatch(match.Uri)

		if match.IgnoreUriCase {
			retval += " uncased"
		}
	}

	if len(match.Headers) > 0 {
		headerConds := []string{}
		for key, val := range match.Headers {
			headerConds = append(headerConds, fmt.Sprintf("%s=%s", key, renderStringMatch(val)))
		}
		retval += " when headers are " + strings.Join(headerConds, "; ")
	}

	// TODO QueryParams, maybe Gateways
	return strings.TrimSpace(retval)
}

func printPod(writer io.Writer, pod *corev1.Pod, revision string) {
	ports := []string{}
	UserID := int64(1337)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			var protocol string
			// Suppress /<protocol> for TCP, print it for everything else
			if port.Protocol != "TCP" {
				protocol = fmt.Sprintf("/%s", port.Protocol)
			}
			ports = append(ports, fmt.Sprintf("%d%s (%s)", port.ContainerPort, protocol, container.Name))
		}
		// Ref: https://istio.io/latest/docs/ops/deployment/requirements/#pod-requirements
		if container.Name != "istio-proxy" && container.Name != "istio-operator" {
			if container.SecurityContext != nil && container.SecurityContext.RunAsUser != nil {
				if *container.SecurityContext.RunAsUser == UserID {
					fmt.Fprintf(writer, "WARNING: User ID (UID) 1337 is reserved for the sidecar proxy.\n")
				}
			}
		}
	}

	fmt.Fprintf(writer, "Pod: %s\n", kname(pod.ObjectMeta))
	fmt.Fprintf(writer, "   Pod Revision: %s\n", revision)
	if len(ports) > 0 {
		fmt.Fprintf(writer, "   Pod Ports: %s\n", strings.Join(ports, ", "))
	} else {
		fmt.Fprintf(writer, "   Pod does not expose ports\n")
	}

	if pod.Status.Phase != corev1.PodRunning {
		fmt.Printf("   Pod is not %s (%s)\n", corev1.PodRunning, pod.Status.Phase)
		return
	}

	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			fmt.Fprintf(writer, "WARNING: Pod %s Container %s NOT READY\n", kname(pod.ObjectMeta), containerStatus.Name)
		}
	}
	for _, containerStatus := range pod.Status.InitContainerStatuses {
		if !containerStatus.Ready {
			fmt.Fprintf(writer, "WARNING: Pod %s Init Container %s NOT READY\n", kname(pod.ObjectMeta), containerStatus.Name)
		}
	}

	if ignoreUnmeshed {
		return
	}

	if !isMeshed(pod) {
		fmt.Fprintf(writer, "WARNING: %s is not part of mesh; no Istio sidecar\n", kname(pod.ObjectMeta))
		return
	}

	// Ref: https://istio.io/latest/docs/ops/deployment/requirements/#pod-requirements
	if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.RunAsUser != nil {
		if *pod.Spec.SecurityContext.RunAsUser == UserID {
			fmt.Fprintf(writer, "   WARNING: User ID (UID) 1337 is reserved for the sidecar proxy.\n")
		}
	}

	// https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
	// says "We recommend adding an explicit app label and version label to deployments."
	if !labels.HasCanonicalServiceName(pod.Labels) {
		fmt.Fprintf(writer, "Suggestion: add required service name label for Istio telemetry. "+
			"See %s.\n", url.DeploymentRequirements)
	}
	if !labels.HasCanonicalServiceRevision(pod.Labels) {
		fmt.Fprintf(writer, "Suggestion: add required service revision label for Istio telemetry. "+
			"See %s.\n", url.DeploymentRequirements)
	}
}

func kname(meta metav1.ObjectMeta) string {
	if meta.Namespace == describeNamespace {
		return meta.Name
	}

	// Use the Istio convention pod-name[.namespace]
	return fmt.Sprintf("%s.%s", meta.Name, meta.Namespace)
}

func printService(writer io.Writer, svc corev1.Service, pod *corev1.Pod) {
	fmt.Fprintf(writer, "Service: %s\n", kname(svc.ObjectMeta))
	for _, port := range svc.Spec.Ports {
		if port.Protocol != "TCP" {
			// Ignore UDP ports, which are not supported by Istio
			continue
		}
		// Get port number
		nport, err := pilotcontroller.FindPort(pod, &port)
		if err == nil {
			protocol := findProtocolForPort(&port)
			fmt.Fprintf(writer, "   Port: %s %d/%s targets pod port %d\n", port.Name, port.Port, protocol, nport)
		} else {
			fmt.Fprintf(writer, "   %s\n", err.Error())
		}
	}
}

func findProtocolForPort(port *corev1.ServicePort) string {
	var protocol string
	if port.Name == "" && port.AppProtocol == nil && port.Protocol != corev1.ProtocolUDP {
		protocol = "auto-detect"
	} else {
		protocol = string(configKube.ConvertProtocol(port.Port, port.Name, port.Protocol, port.AppProtocol))
		if protocol == protocolinstance.Unsupported.String() && port.AppProtocol != nil && *port.AppProtocol == "hbone" {
			// HBONE is used for some internal code.
			protocol = string(protocolinstance.HBONE)
		}
	}
	return protocol
}

func isMeshed(pod *corev1.Pod) bool {
	return inject.FindSidecar(pod) != nil
}

// Extract value of key out of Struct, but always return a Struct, even if the value isn't one
func (v *myProtoValue) keyAsStruct(key string) *myProtoValue {
	if v == nil || v.GetStructValue() == nil {
		return asMyProtoValue(&structpb.Struct{Fields: make(map[string]*structpb.Value)})
	}

	return &myProtoValue{v.GetStructValue().Fields[key]}
}

// asMyProtoValue wraps a protobuf Struct so we may use it with keyAsStruct and keyAsString
func asMyProtoValue(s *structpb.Struct) *myProtoValue {
	return &myProtoValue{
		&structpb.Value{
			Kind: &structpb.Value_StructValue{
				StructValue: s,
			},
		},
	}
}

func (v *myProtoValue) keyAsString(key string) string {
	s := v.keyAsStruct(key)
	return s.GetStringValue()
}

func getIstioRBACPolicies(cd *configdump.Wrapper, port int32) ([]string, error) {
	hcm, err := getInboundHTTPConnectionManager(cd, port)
	if err != nil || hcm == nil {
		return []string{}, err
	}

	// Identify RBAC policies. Currently there are no "breadcrumbs" so we only return the policy names.
	for _, httpFilter := range hcm.HttpFilters {
		if httpFilter.Name == wellknown.HTTPRoleBasedAccessControl {
			rbac := &rbachttp.RBAC{}
			if err := httpFilter.GetTypedConfig().UnmarshalTo(rbac); err == nil {
				policies := []string{}
				for polName := range rbac.Rules.Policies {
					policies = append(policies, polName)
				}
				return policies, nil
			}
		}
	}

	return []string{}, nil
}

// Return the first HTTP Connection Manager config for the inbound port
func getInboundHTTPConnectionManager(cd *configdump.Wrapper, port int32) (*hcm.HttpConnectionManager, error) {
	filter := istio_envoy_configdump.ListenerFilter{
		Port: uint32(port),
	}
	listeners, err := cd.GetListenerConfigDump()
	if err != nil {
		return nil, err
	}

	for _, l := range listeners.DynamicListeners {
		if l.ActiveState == nil {
			continue
		}
		// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
		l.ActiveState.Listener.TypeUrl = v3.ListenerType
		listenerTyped := &listener.Listener{}
		err = l.ActiveState.Listener.UnmarshalTo(listenerTyped)
		if err != nil {
			return nil, err
		}
		if listenerTyped.Name == model.VirtualInboundListenerName {
			for _, filterChain := range listenerTyped.FilterChains {
				for _, filter := range filterChain.Filters {
					hcm := &hcm.HttpConnectionManager{}
					if err := filter.GetTypedConfig().UnmarshalTo(hcm); err == nil {
						return hcm, nil
					}
				}
			}
		}
		// This next check is deprecated in 1.6 and can be removed when we remove
		// the old config_dumps in support of https://github.com/istio/istio/issues/23042
		if filter.Verify(listenerTyped) {
			sockAddr := listenerTyped.Address.GetSocketAddress()
			if sockAddr != nil {
				// Skip outbound listeners
				if sockAddr.Address == "0.0.0.0" {
					continue
				}
			}

			for _, filterChain := range listenerTyped.FilterChains {
				for _, filter := range filterChain.Filters {
					hcm := &hcm.HttpConnectionManager{}
					if err := filter.GetTypedConfig().UnmarshalTo(hcm); err == nil {
						return hcm, nil
					}
				}
			}
		}
	}

	return nil, nil
}

// getIstioVirtualServiceNameForSvc returns name, namespace
func getIstioVirtualServiceNameForSvc(cd *configdump.Wrapper, svc corev1.Service, port int32) (string, string, error) {
	path, err := getIstioVirtualServicePathForSvcFromRoute(cd, svc, port)
	if err != nil {
		return "", "", err
	}

	// Starting with recent 1.5.0 builds, the path will include .istio.io.  Handle both.
	// nolint: gosimple
	re := regexp.MustCompile("/apis/networking(\\.istio\\.io)?/v1(?:alpha3)?/namespaces/(?P<namespace>[^/]+)/virtual-service/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a VS path: %s", path)
	}
	return ss[3], ss[2], nil
}

// getIstioVirtualServicePathForSvcFromRoute returns something like "/apis/networking/v1alpha3/namespaces/default/virtual-service/reviews"
func getIstioVirtualServicePathForSvcFromRoute(cd *configdump.Wrapper, svc corev1.Service, port int32) (string, error) {
	sPort := strconv.Itoa(int(port))

	// Routes know their destination Service name, namespace, and port, and the DR that configures them
	rcd, err := cd.GetDynamicRouteDump(false)
	if err != nil {
		return "", err
	}
	for _, rcd := range rcd.DynamicRouteConfigs {
		routeTyped := &route.RouteConfiguration{}
		err = rcd.RouteConfig.UnmarshalTo(routeTyped)
		if err != nil {
			return "", err
		}
		if routeTyped.Name != sPort && !strings.HasPrefix(routeTyped.Name, "http.") &&
			!strings.HasPrefix(routeTyped.Name, "https.") {
			continue
		}

		for _, vh := range routeTyped.VirtualHosts {
			for _, route := range vh.Routes {
				if routeDestinationMatchesSvc(route, svc, vh, port) {
					return getIstioConfig(route.Metadata)
				}
			}
		}
	}
	return "", nil
}

// routeDestinationMatchesSvc determines whether or not to use this service as a destination
func routeDestinationMatchesSvc(vhRoute *route.Route, svc corev1.Service, vh *route.VirtualHost, port int32) bool {
	if vhRoute == nil {
		return false
	}

	// Infer from VirtualHost domains matching <service>.<namespace>.svc.cluster.local
	re := regexp.MustCompile(`(?P<service>[^\.]+)\.(?P<namespace>[^\.]+)\.svc\.cluster\.local$`)
	for _, domain := range vh.Domains {
		ss := re.FindStringSubmatch(domain)
		if ss != nil {
			if ss[1] == svc.ObjectMeta.Name && ss[2] == svc.ObjectMeta.Namespace {
				return true
			}
		}
	}

	clusterName := ""
	switch cs := vhRoute.GetRoute().GetClusterSpecifier().(type) {
	case *route.RouteAction_Cluster:
		clusterName = cs.Cluster
	case *route.RouteAction_WeightedClusters:
		clusterName = cs.WeightedClusters.Clusters[0].GetName()
	}

	// If this is an ingress gateway, the Domains will be something like *:80, so check routes
	// which will look like "outbound|9080||productpage.default.svc.cluster.local"
	res := fmt.Sprintf(`outbound\|%d\|[^\|]*\|(?P<service>[^\.]+)\.(?P<namespace>[^\.]+)\.svc\.cluster\.local$`, port)
	re = regexp.MustCompile(res)

	ss := re.FindStringSubmatch(clusterName)
	if ss != nil {
		if ss[1] == svc.ObjectMeta.Name && ss[2] == svc.ObjectMeta.Namespace {
			return true
		}
	}

	return false
}

// getIstioConfig returns .metadata.filter_metadata.istio.config, err
func getIstioConfig(metadata *core.Metadata) (string, error) {
	if metadata != nil {
		istioConfig := asMyProtoValue(metadata.FilterMetadata[util.IstioMetadataKey]).
			keyAsString("config")
		return istioConfig, nil
	}
	return "", fmt.Errorf("no istio config")
}

// getIstioDestinationRuleNameForSvc returns name, namespace
func getIstioDestinationRuleNameForSvc(cd *configdump.Wrapper, svc corev1.Service, port int32) (string, string, error) {
	path, err := getIstioDestinationRulePathForSvc(cd, svc, port)
	if err != nil || path == "" {
		return "", "", err
	}

	// Starting with recent 1.5.0 builds, the path will include .istio.io.  Handle both.
	// nolint: gosimple
	re := regexp.MustCompile("/apis/networking(\\.istio\\.io)?/v1(?:alpha3)?/namespaces/(?P<namespace>[^/]+)/destination-rule/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a DR path: %s", path)
	}
	return ss[3], ss[2], nil
}

// getIstioDestinationRulePathForSvc returns something like "/apis/networking/v1alpha3/namespaces/default/destination-rule/reviews"
func getIstioDestinationRulePathForSvc(cd *configdump.Wrapper, svc corev1.Service, port int32) (string, error) {
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	filter := istio_envoy_configdump.ClusterFilter{
		FQDN: host.Name(svcHost),
		Port: int(port),
		// Although we want inbound traffic, ask for outbound traffic, as the DR is
		// not associated with the inbound traffic.
		Direction: model.TrafficDirectionOutbound,
	}

	dump, err := cd.GetClusterConfigDump()
	if err != nil {
		return "", err
	}

	for _, dac := range dump.DynamicActiveClusters {
		clusterTyped := &cluster.Cluster{}
		// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
		dac.Cluster.TypeUrl = v3.ClusterType
		err = dac.Cluster.UnmarshalTo(clusterTyped)
		if err != nil {
			return "", err
		}
		if filter.Verify(clusterTyped) {
			metadata := clusterTyped.Metadata
			if metadata != nil {
				istioConfig := asMyProtoValue(metadata.FilterMetadata[util.IstioMetadataKey]).
					keyAsString("config")
				return istioConfig, nil
			}
		}
	}

	return "", nil
}

// TODO simplify this by showing for each matching Destination the negation of the previous HttpMatchRequest
// and showing the non-matching Destinations.  (The current code is ad-hoc, and usually shows most of that information.)
func printVirtualService(writer io.Writer, initPrintNum int,
	vs *clientnetworking.VirtualService, svc corev1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *clientnetworking.DestinationRule,
) { // nolint: lll
	fmt.Fprintf(writer, "%sVirtualService: %s\n", printSpaces(initPrintNum+printLevel0), kname(vs.ObjectMeta))

	// There is no point in checking that 'port' uses HTTP (for HTTP route matches)
	// or uses TCP (for TCP route matches) because if the port has the wrong name
	// the VirtualService metadata will not appear.

	matches := 0
	facts := 0
	mismatchNotes := []string{}
	for _, httpRoute := range vs.Spec.Http {
		routeMatch, newfacts := httpRouteMatchSvc(vs, httpRoute, svc, matchingSubsets, nonmatchingSubsets, dr)
		if routeMatch {
			matches++
			for _, newfact := range newfacts {
				fmt.Fprintf(writer, "%s%s\n", printSpaces(initPrintNum+printLevel1), newfact)
				facts++
			}
		} else {
			mismatchNotes = append(mismatchNotes, newfacts...)
		}
	}

	// TODO vsSpec.Tls if I can find examples in the wild

	for _, tcpRoute := range vs.Spec.Tcp {
		routeMatch, newfacts := tcpRouteMatchSvc(vs, tcpRoute, svc)
		if routeMatch {
			matches++
			for _, newfact := range newfacts {
				fmt.Fprintf(writer, "%s%s\n", printSpaces(initPrintNum+printLevel1), newfact)
				facts++
			}
		} else {
			mismatchNotes = append(mismatchNotes, newfacts...)
		}
	}

	if matches == 0 {
		if len(vs.Spec.Http) > 0 {
			fmt.Fprintf(writer, "%sWARNING: No destinations match pod subsets (checked %d HTTP routes)\n",
				printSpaces(initPrintNum+printLevel1), len(vs.Spec.Http))
		}
		if len(vs.Spec.Tcp) > 0 {
			fmt.Fprintf(writer, "%sWARNING: No destinations match pod subsets (checked %d TCP routes)\n",
				printSpaces(initPrintNum+printLevel1), len(vs.Spec.Tcp))
		}
		for _, mismatch := range mismatchNotes {
			fmt.Fprintf(writer, "%s%s\n",
				printSpaces(initPrintNum+printLevel2), mismatch)
		}
		return
	}

	possibleDests := len(vs.Spec.Http) + len(vs.Spec.Tls) + len(vs.Spec.Tcp)
	if matches < possibleDests {
		// We've printed the match conditions.  We can't say for sure that matching
		// traffic will reach this pod, because an earlier match condition could have captured it.
		fmt.Fprintf(writer, "%s%d additional destination(s) that will not reach this pod\n",
			printSpaces(initPrintNum+printLevel1), possibleDests-matches)
		// If we matched, but printed nothing, treat this as the catch-all
		if facts == 0 {
			for _, mismatch := range mismatchNotes {
				fmt.Fprintf(writer, "%s%s\n",
					printSpaces(initPrintNum+printLevel2), mismatch)
			}
		}

		return
	}

	if facts == 0 {
		// We printed nothing other than the name.  Print something.
		if len(vs.Spec.Http) > 0 {
			fmt.Fprintf(writer, "%s%d HTTP route(s)\n", printSpaces(initPrintNum+printLevel1), len(vs.Spec.Http))
		}
		if len(vs.Spec.Tcp) > 0 {
			fmt.Fprintf(writer, "%s%d TCP route(s)\n", printSpaces(initPrintNum+printLevel1), len(vs.Spec.Tcp))
		}
	}
}

type ingressInfo struct {
	service *corev1.Service
	pods    []*corev1.Pod
}

func (ingress *ingressInfo) match(gw *clientnetworking.Gateway) bool {
	if ingress == nil || gw == nil {
		return false
	}
	if gw.Spec.Selector == nil {
		return true
	}
	for _, p := range ingress.pods {
		if maps.Contains(p.GetLabels(), gw.Spec.Selector) {
			return true
		}
	}
	return false
}

func (ingress *ingressInfo) getIngressIP() string {
	if ingress == nil || ingress.service == nil || len(ingress.pods) == 0 {
		return "unknown"
	}

	if len(ingress.service.Status.LoadBalancer.Ingress) > 0 {
		return ingress.service.Status.LoadBalancer.Ingress[0].IP
	}

	if hIP := ingress.pods[0].Status.HostIP; hIP != "" {
		return hIP
	}

	// The scope of this function is to get the IP from Kubernetes, we do not
	// ask Docker or minikube for an IP.
	// See https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/#determining-the-ingress-ip-and-ports
	return "unknown"
}

func printIngressInfo(
	writer io.Writer,
	matchingServices []corev1.Service,
	podsLabels []klabels.Set,
	kubeClient kubernetes.Interface,
	configClient istioclient.Interface,
	client kube.CLIClient,
) error {
	pods, err := kubeClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "istio=ingressgateway",
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return multierror.Prefix(err, "Could not find ingress gateway pods")
	}
	if len(pods.Items) == 0 {
		fmt.Fprintf(writer, "Skipping Gateway information (no ingress gateway pods)\n")
		return nil
	}
	// key: namespace
	ingressPods := map[string][]*corev1.Pod{}
	ingressNss := sets.New[string]()
	for i, pod := range pods.Items {
		ns := pod.GetNamespace()
		ingressNss.Insert(ns)
		ingressPods[ns] = append(ingressPods[ns], pods.Items[i].DeepCopy())
	}

	foundIngresses := []*ingressInfo{}
	for _, ns := range ingressNss.UnsortedList() {
		// Currently no support for non-standard gateways selecting non ingressgateway pods
		serviceList, err := kubeClient.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		if err == nil {
			for i, s := range serviceList.Items {
				iInfo := &ingressInfo{
					service: serviceList.Items[i].DeepCopy(),
				}
				for j, p := range ingressPods[ns] {
					if p.GetLabels() == nil {
						continue
					}
					if maps.Contains(p.GetLabels(), s.Spec.Selector) {
						iInfo.pods = append(iInfo.pods, ingressPods[ns][j])
					}
				}
				if len(iInfo.pods) > 0 {
					foundIngresses = append(foundIngresses, iInfo)
				}
			}
		}
	}

	if len(foundIngresses) == 0 {
		fmt.Fprintf(writer, "Skipping Gateway information (no ingress gateway service)\n")
	}

	newResourceID := func(ns, name string) string { return fmt.Sprintf("%s/%s", ns, name) }
	recordVirtualServices := map[string]*clientnetworking.VirtualService{}
	recordDestinationRules := map[string]*clientnetworking.DestinationRule{}
	// recordGateways, key: ns/gwName
	recordGateways := map[string]bool{}

	for _, pod := range pods.Items {
		byConfigDump, err := client.EnvoyDo(context.TODO(), pod.Name, pod.Namespace, "GET", "config_dump")
		if err != nil {
			return fmt.Errorf("failed to execute command on ingress gateway sidecar: %v", err)
		}
		cd := configdump.Wrapper{}
		err = cd.UnmarshalJSON(byConfigDump)
		if err != nil {
			return fmt.Errorf("can't parse ingress gateway sidecar config_dump: %v", err)
		}

		for _, svc := range matchingServices {
			for _, port := range svc.Spec.Ports {
				// found destination rule and matching subsets
				matchingSubsets := []string{}
				nonMatchingSubsets := []string{}
				drName, drNamespace, err := getIstioDestinationRuleNameForSvc(&cd, svc, port.Port)
				var dr *clientnetworking.DestinationRule
				if err == nil && drName != "" && drNamespace != "" {
					exist := false
					dr, exist = recordDestinationRules[newResourceID(drNamespace, drName)]
					if !exist {
						dr, _ = configClient.NetworkingV1().DestinationRules(drNamespace).Get(context.Background(), drName, metav1.GetOptions{})
						if dr == nil {
							fmt.Fprintf(writer,
								"WARNING: Proxy is stale; it references to non-existent destination rule %s.%s\n",
								drName, drNamespace)
						}
						recordDestinationRules[newResourceID(drNamespace, drName)] = dr.DeepCopy()
					}
				}
				if dr != nil {
					matchingSubsets, nonMatchingSubsets = getDestRuleSubsets(dr.Spec.Subsets, podsLabels)
				}

				// found virtual service
				vsName, vsNamespace, err := getIstioVirtualServiceNameForSvc(&cd, svc, port.Port)
				var vs *clientnetworking.VirtualService
				if err == nil && vsName != "" && vsNamespace != "" {
					exist := false
					vs, exist = recordVirtualServices[newResourceID(vsNamespace, vsName)]
					if !exist {
						vs, _ = configClient.NetworkingV1().VirtualServices(vsNamespace).Get(context.Background(), vsName, metav1.GetOptions{})
						if vs == nil {
							fmt.Fprintf(writer,
								"WARNING: Proxy is stale; it references to non-existent virtual service %s.%s\n",
								vsName, vsNamespace)
						}
						recordVirtualServices[newResourceID(vsNamespace, vsName)] = vs.DeepCopy()
					}
					if vs != nil {
						// Matching gateways from vs.spec.gateways
						for _, gatewayName := range vs.Spec.Gateways {
							if gatewayName == "" || gatewayName == analyzerutil.MeshGateway {
								continue
							}
							// parse gateway
							gns := vsNamespace
							parts := strings.SplitN(gatewayName, "/", 2)
							if len(parts) == 2 {
								gatewayName = parts[1]
								gns = parts[0]
							}
							// todo: check istiod env `PILOT_SCOPE_GATEWAY_TO_NAMESPACE`, if true, need to match gateway namespace

							gwID := newResourceID(gns, gatewayName)
							if gok := recordGateways[gwID]; !gok {
								gw, _ := configClient.NetworkingV1().Gateways(gns).Get(context.Background(), gatewayName, metav1.GetOptions{})
								if gw != nil {
									recordGateways[gwID] = true
									if gw.Spec.Selector == nil {
										fmt.Fprintf(writer,
											"Ingress Gateway %s/%s be applyed all workloads",
											gns, gatewayName)
										continue
									}

									var matchIngressInfos []*ingressInfo
									for i, ingress := range foundIngresses {
										if ingress.match(gw) {
											matchIngressInfos = append(matchIngressInfos, foundIngresses[i])
										}
									}
									if len(matchIngressInfos) > 0 {
										sort.Slice(matchIngressInfos, func(i, j int) bool {
											return matchIngressInfos[i].getIngressIP() < matchIngressInfos[j].getIngressIP()
										})
										fmt.Fprintf(writer, "--------------------\n")
										for _, ingress := range matchIngressInfos {
											printIngressService(writer, printLevel0, ingress)
										}
										printVirtualService(writer, printLevel0, vs, svc, matchingSubsets, nonMatchingSubsets, dr)
									}
								} else {
									fmt.Fprintf(writer,
										"WARNING: Proxy is stale; it references to non-existent gateway %s/%s\n",
										gns, gatewayName)
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func printIngressService(writer io.Writer, initPrintNum int,
	ingress *ingressInfo,
) {
	if ingress == nil || ingress.service == nil || len(ingress.pods) == 0 {
		return
	}
	// The ingressgateway service offers a lot of ports but the pod doesn't listen to all
	// of them.  For example, it doesn't listen on 443 without additional setup.  This prints
	// the most basic output.
	portsToShow := map[string]bool{
		"http2": true,
		"http":  true,
	}
	protocolToScheme := map[string]string{
		"HTTP2": "http",
		"HTTP":  "http",
	}
	schemePortDefault := map[string]int{
		"http": 80,
	}

	for _, port := range ingress.service.Spec.Ports {
		if port.Protocol != "TCP" || !portsToShow[port.Name] {
			continue
		}

		// Get port number
		_, err := pilotcontroller.FindPort(ingress.pods[0], &port)
		if err == nil {
			nport := int(port.Port)
			protocol := string(configKube.ConvertProtocol(port.Port, port.Name, port.Protocol, port.AppProtocol))

			scheme := protocolToScheme[protocol]
			portSuffix := ""
			if schemePortDefault[scheme] != nport {
				portSuffix = fmt.Sprintf(":%d", nport)
			}
			ip := ingress.getIngressIP()
			fmt.Fprintf(writer, "%sExposed on Ingress Gateway %s://%s%s\n", printSpaces(initPrintNum), scheme, ip, portSuffix)
		}
	}
}

func svcDescribeCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "service <svc-name>[.<namespace>]",
		Aliases: []string{"svc"},
		Short:   "Describe services and their Istio configuration [kube-only]",
		Long: `Analyzes service, pods, DestinationRules, and VirtualServices and reports
the configuration objects that affect that service.`,
		Example: `  #Service query with inferred namespace (current context's namespace)
  istioctl experimental describe service productpage

  # Service query with explicit namespace
  istioctl experimental describe service istio-ingressgateway.istio-system
  or
  istioctl experimental describe service istio-ingressgateway -n istio-system`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("expecting service name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			describeNamespace = ctx.NamespaceOrDefault(ctx.Namespace())
			svcName, ns := handlers.InferPodInfo(args[0], describeNamespace)

			client, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			svc, err := client.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()

			labels := make([]string, 0)
			for k, v := range svc.Spec.Selector {
				labels = append(labels, fmt.Sprintf("%s=%s", k, v))
			}

			matchingPods := make([]corev1.Pod, 0)
			var selectedPodCount int
			if len(labels) > 0 {
				pods, err := client.Kube().CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
					LabelSelector: strings.Join(labels, ","),
				})
				if err != nil {
					return err
				}
				selectedPodCount = len(pods.Items)
				for _, pod := range pods.Items {
					if pod.Status.Phase != corev1.PodRunning {
						fmt.Printf("   Pod is not %s (%s)\n", corev1.PodRunning, pod.Status.Phase)
						continue
					}

					ready, err := containerReady(&pod, inject.ProxyContainerName)
					if err != nil {
						fmt.Fprintf(writer, "Pod %s: %s\n", kname(pod.ObjectMeta), err)
						continue
					}
					if !ready {
						fmt.Fprintf(writer, "WARNING: Pod %s Container %s NOT READY\n", kname(pod.ObjectMeta), inject.ProxyContainerName)
						continue
					}
					matchingPods = append(matchingPods, pod)
				}
			}

			if len(matchingPods) == 0 {
				if selectedPodCount == 0 {
					fmt.Fprintf(writer, "Service %q has no pods.\n", kname(svc.ObjectMeta))
					return nil
				}
				fmt.Fprintf(writer, "Service %q has no Istio pods.  (%d pods in service).\n", kname(svc.ObjectMeta), selectedPodCount)
				fmt.Fprintf(writer, "Use `istioctl kube-inject` or redeploy with Istio automatic sidecar injection.\n")
				return nil
			}

			kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return err
			}

			configClient := client.Istio()

			// Get all the labels for all the matching pods.  We will used this to complain
			// if NONE of the pods match a VirtualService
			podsLabels := make([]klabels.Set, len(matchingPods))
			for i, pod := range matchingPods {
				podsLabels[i] = klabels.Set(pod.ObjectMeta.Labels)
			}

			// Describe based on the Envoy config for this first pod only
			pod := matchingPods[0]

			// Only consider the service invoked with this command, not other services that might select the pod
			svcs := []corev1.Service{*svc}

			err = describePodServices(writer, kubeClient, configClient, &pod, svcs, podsLabels)
			if err != nil {
				return err
			}

			// Now look for ingress gateways
			return printIngressInfo(writer, svcs, podsLabels, client.Kube(), configClient, kubeClient)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidServiceArgs(cmd, ctx, args, toComplete)
		},
	}

	cmd.PersistentFlags().BoolVar(&ignoreUnmeshed, "ignoreUnmeshed", false,
		"Suppress warnings for unmeshed pods")
	cmd.Long += "\n\n" + istioctlutil.ExperimentalMsg
	return cmd
}

func describePodServices(writer io.Writer, kubeClient kube.CLIClient, configClient istioclient.Interface, pod *corev1.Pod, matchingServices []corev1.Service, podsLabels []klabels.Set) error { // nolint: lll
	byConfigDump, err := kubeClient.EnvoyDo(context.TODO(), pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, "GET", "config_dump")
	if err != nil {
		if ignoreUnmeshed {
			return nil
		}

		return fmt.Errorf("failed to execute command on sidecar: %v", err)
	}

	cd := configdump.Wrapper{}
	err = cd.UnmarshalJSON(byConfigDump)
	if err != nil {
		return fmt.Errorf("can't parse sidecar config_dump for %v: %v", err, pod.ObjectMeta.Name)
	}

	for row, svc := range matchingServices {
		if row != 0 {
			fmt.Fprintf(writer, "--------------------\n")
		}
		printService(writer, svc, pod)

		needPrintPort := false
		initPolicyLevel := printLevel0
		if len(svc.Spec.Ports) > 1 {
			needPrintPort = true
			initPolicyLevel = printLevel1
		}
		for _, port := range svc.Spec.Ports {
			if needPrintPort {
				// If there is more than one port, prefix each DR by the port it applies to
				fmt.Fprintf(writer, "%d:\n", port.Port)
			}
			matchingSubsets := []string{}
			nonmatchingSubsets := []string{}
			drName, drNamespace, err := getIstioDestinationRuleNameForSvc(&cd, svc, port.Port)
			if err != nil {
				log.Errorf("fetch destination rule for %v: %v", svc.Name, err)
			}
			var dr *clientnetworking.DestinationRule
			if err == nil && drName != "" && drNamespace != "" {
				dr, _ = configClient.NetworkingV1().DestinationRules(drNamespace).Get(context.Background(), drName, metav1.GetOptions{})
				if dr != nil {
					printDestinationRule(writer, initPolicyLevel, dr, podsLabels)
					matchingSubsets, nonmatchingSubsets = getDestRuleSubsets(dr.Spec.Subsets, podsLabels)
				} else {
					fmt.Fprintf(writer,
						"WARNING: Proxy is stale; it references to non-existent destination rule %s.%s\n",
						drName, drNamespace)
				}
			}

			vsName, vsNamespace, err := getIstioVirtualServiceNameForSvc(&cd, svc, port.Port)
			if err == nil && vsName != "" && vsNamespace != "" {
				vs, _ := configClient.NetworkingV1().VirtualServices(vsNamespace).Get(context.Background(), vsName, metav1.GetOptions{})
				if vs != nil {
					printVirtualService(writer, initPolicyLevel, vs, svc, matchingSubsets, nonmatchingSubsets, dr)
				} else {
					fmt.Fprintf(writer,
						"WARNING: Proxy is stale; it references to non-existent virtual service %s.%s\n",
						vsName, vsNamespace)
				}
			}

			policies, err := getIstioRBACPolicies(&cd, port.Port)
			if err != nil {
				log.Errorf("error getting rbac policies: %v", err)
			}
			if len(policies) > 0 {
				if len(svc.Spec.Ports) > 1 {
					// If there is more than one port, prefix each DR by the port it applies to
					fmt.Fprintf(writer, "%d ", port.Port)
				}

				fmt.Fprintf(writer, "RBAC policies: %s\n", strings.Join(policies, ", "))
			}
		}
	}

	return nil
}

func containerReady(pod *corev1.Pod, containerName string) (bool, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.Ready, nil
		}
	}
	for _, containerStatus := range pod.Status.InitContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.Ready, nil
		}
	}
	return false, fmt.Errorf("no container %q in pod", containerName)
}

// describePeerAuthentication fetches all PeerAuthentication in workload and root namespace.
// It lists the ones applied to the pod, and the current active mTLS mode.
// When the client doesn't have access to root namespace, it will only show workload namespace Peerauthentications.
func describePeerAuthentication(
	writer io.Writer,
	kubeClient kube.CLIClient,
	configClient istioclient.Interface,
	workloadNamespace string,
	podsLabels klabels.Set,
	istioNamespace string,
) error {
	meshCfg, err := getMeshConfig(kubeClient, istioNamespace)
	if err != nil {
		return fmt.Errorf("failed to fetch mesh config: %v", err)
	}

	workloadPAList, err := configClient.SecurityV1().PeerAuthentications(workloadNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch workload namespace PeerAuthentication: %v", err)
	}

	rootPAList, err := configClient.SecurityV1().PeerAuthentications(meshCfg.RootNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch root namespace PeerAuthentication: %v", err)
	}

	allPAs := append(rootPAList.Items, workloadPAList.Items...)

	var cfgs []*config.Config
	for _, pa := range allPAs {
		cfg := crdclient.TranslateObject(pa, config.GroupVersionKind(pa.GroupVersionKind()), "")
		cfgs = append(cfgs, &cfg)
	}

	matchedPA := findMatchedConfigs(podsLabels, cfgs)
	effectivePA := authn.ComposePeerAuthentication(meshCfg.RootNamespace, matchedPA)
	printPeerAuthentication(writer, effectivePA)
	if len(matchedPA) != 0 {
		printConfigs(writer, matchedPA)
	}

	return nil
}

// Workloader is used for matching all configs
type Workloader interface {
	GetSelector() *typev1beta1.WorkloadSelector
}

// findMatchedConfigs should filter out unrelated configs that are not matched given podsLabels.
// When the config has no selector labels, this method will treat it as qualified namespace level
// config. So configs passed into this method should only contains workload's namespaces configs
// and rootNamespaces configs, caller should be responsible for controlling configs passed
// in.
func findMatchedConfigs(podsLabels klabels.Set, configs []*config.Config) []*config.Config {
	var cfgs []*config.Config

	for _, cfg := range configs {
		labels := cfg.Spec.(Workloader).GetSelector().GetMatchLabels()
		selector := klabels.SelectorFromSet(labels)
		if selector.Matches(podsLabels) {
			cfgs = append(cfgs, cfg)
		}
	}

	return cfgs
}

// printConfigs prints the applied configs based on the member's type.
// When there is the array is empty, caller should make sure the intended
// log is handled in their methods.
func printConfigs(writer io.Writer, configs []*config.Config) {
	if len(configs) == 0 {
		return
	}
	fmt.Fprintf(writer, "Applied %s:\n", configs[0].Meta.GroupVersionKind.Kind)
	var cfgNames string
	for i, cfg := range configs {
		cfgNames += cfg.Meta.Name + "." + cfg.Meta.Namespace
		if i < len(configs)-1 {
			cfgNames += ", "
		}
	}
	fmt.Fprintf(writer, "   %s\n", cfgNames)
}

func printPeerAuthentication(writer io.Writer, pa authn.MergedPeerAuthentication) {
	fmt.Fprintf(writer, "Effective PeerAuthentication:\n")
	fmt.Fprintf(writer, "   Workload mTLS mode: %s\n", pa.Mode.String())
	if len(pa.PerPort) != 0 {
		fmt.Fprintf(writer, "   Port Level mTLS mode:\n")
		for port, mode := range pa.PerPort {
			fmt.Fprintf(writer, "      %d: %s\n", port, mode.String())
		}
	}
}

func getMeshConfig(kubeClient kube.CLIClient, istioNamespace string) (*meshconfig.MeshConfig, error) {
	rev := kubeClient.Revision()
	meshConfigMapName := istioctlutil.DefaultMeshConfigMapName

	// if the revision is not "default", render mesh config map name with revision
	if rev != "default" && rev != "" {
		meshConfigMapName = fmt.Sprintf("%s-%s", istioctlutil.DefaultMeshConfigMapName, rev)
	}

	meshConfigMap, err := kubeClient.Kube().CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not read configmap %q from namespace %q: %v", meshConfigMapName, istioNamespace, err)
	}

	configYaml, ok := meshConfigMap.Data[istioctlutil.ConfigMapKey]
	if !ok {
		return nil, fmt.Errorf("missing config map key %q", istioctlutil.ConfigMapKey)
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		return nil, fmt.Errorf("error parsing mesh config: %v", err)
	}

	return cfg, nil
}

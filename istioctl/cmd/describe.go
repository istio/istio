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
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_api_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rbac_http_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	clientnetworking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	mixerclient "istio.io/api/mixer/v1/config/client"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	istio_envoy_configdump "istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	"istio.io/istio/pilot/pkg/model"
	pilot_v1alpha3 "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	pilotcontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
)

type myProtoValue struct {
	*structpb.Value
}

const (
	k8sSuffix = ".svc." + constants.DefaultKubernetesDomain
)

var (
	// Function that creates Kubernetes client-go; making it a variable lets us mock client-go
	interfaceFactory = createInterface

	// Ignore unmeshed pods.  This makes it easy to suppress warnings about kube-system etc
	ignoreUnmeshed = false
)

func podDescribeCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "pod <pod>",
		Short: "Describe pods and their Istio configuration [kube-only]",
		Long: `Analyzes pod, its Services, DestinationRules, and VirtualServices and reports
the configuration objects that affect that pod.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental describe pod productpage-v1-c7765c886-7zzd4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting pod name")
			}

			podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))

			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			pod, err := client.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()

			podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)

			printPod(writer, pod)

			svcs, err := client.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			matchingServices := []v1.Service{}
			for _, svc := range svcs.Items {
				if len(svc.Spec.Selector) > 0 {
					svcSelector := k8s_labels.SelectorFromSet(svc.Spec.Selector)
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

			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}

			var configClient istioclient.Interface
			if configClient, err = configStoreFactory(); err != nil {
				return err
			}

			podsLabels := []k8s_labels.Set{k8s_labels.Set(pod.ObjectMeta.Labels)}
			fmt.Fprintf(writer, "--------------------\n")
			err = describePodServices(writer, kubeClient, configClient, pod, matchingServices, podsLabels)
			if err != nil {
				return err
			}

			// TODO find sidecar configs that select this workload and render them

			// Now look for ingress gateways
			return printIngressInfo(writer, matchingServices, podsLabels, client, configClient, kubeClient)
		},
	}

	cmd.PersistentFlags().BoolVar(&ignoreUnmeshed, "ignoreUnmeshed", false,
		"Suppress warnings for unmeshed pods")

	return cmd
}

func describe() *cobra.Command {
	describeCmd := &cobra.Command{
		Use:     "describe",
		Aliases: []string{"des"},
		Short:   "Describe resource and related Istio configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown resource type %q", args[0])
			}

			return nil
		},
	}

	describeCmd.AddCommand(podDescribeCmd())
	describeCmd.AddCommand(svcDescribeCmd())
	return describeCmd
}

func validatePort(port v1.ServicePort, pod *v1.Pod) []string {
	retval := []string{}

	// Build list of ports exposed by pod
	containerPorts := make(map[int]bool)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			containerPorts[int(port.ContainerPort)] = true
		}
	}

	// Get port number used by the service port being validated
	_, err := pilotcontroller.FindPort(pod, &port)
	if err != nil {
		retval = append(retval, err.Error())
	}

	return retval
}

func servicePortProtocol(name string) protocol.Instance {
	i := strings.IndexByte(name, '-')
	if i >= 0 {
		name = name[:i]
	}
	return protocol.Parse(name)
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
func getDestRuleSubsets(subsets []*v1alpha3.Subset, podsLabels []k8s_labels.Set) ([]string, []string) {
	matchingSubsets := []string{}
	nonmatchingSubsets := []string{}
	for _, subset := range subsets {
		subsetSelector := k8s_labels.SelectorFromSet(subset.Labels)
		if matchesAnyPod(subsetSelector, podsLabels) {
			matchingSubsets = append(matchingSubsets, subset.Name)
		} else {
			nonmatchingSubsets = append(nonmatchingSubsets, subset.Name)
		}
	}

	return matchingSubsets, nonmatchingSubsets
}

func matchesAnyPod(subsetSelector k8s_labels.Selector, podsLabels []k8s_labels.Set) bool {
	for _, podLabels := range podsLabels {
		if subsetSelector.Matches(podLabels) {
			return true
		}
	}
	return false
}

func printDestinationRule(writer io.Writer, dr *clientnetworking.DestinationRule, podsLabels []k8s_labels.Set) {
	fmt.Fprintf(writer, "DestinationRule: %s for %q\n", kname(dr.ObjectMeta), dr.Spec.Host)

	matchingSubsets, nonmatchingSubsets := getDestRuleSubsets(dr.Spec.Subsets, podsLabels)

	if len(matchingSubsets) != 0 || len(nonmatchingSubsets) != 0 {
		if len(matchingSubsets) == 0 {
			fmt.Fprintf(writer, "  WARNING POD DOES NOT MATCH ANY SUBSETS.  (Non matching subsets %s)\n",
				strings.Join(nonmatchingSubsets, ","))
		}
		fmt.Fprintf(writer, "   Matching subsets: %s\n", strings.Join(matchingSubsets, ","))
		if len(nonmatchingSubsets) > 0 {
			fmt.Fprintf(writer, "      (Non-matching subsets %s)\n", strings.Join(nonmatchingSubsets, ","))
		}
	}

	// Ignore LoadBalancer, ConnectionPool, OutlierDetection, and PortLevelSettings
	trafficPolicy := dr.Spec.TrafficPolicy
	if trafficPolicy == nil {
		fmt.Fprintf(writer, "   No Traffic Policy\n")
	} else {
		if trafficPolicy.Tls != nil {
			fmt.Fprintf(writer, "   Traffic Policy TLS Mode: %s\n", dr.Spec.TrafficPolicy.Tls.Mode.String())
		}
		extra := []string{}
		if trafficPolicy.LoadBalancer != nil {
			extra = append(extra, "load balancer")
		}
		if trafficPolicy.ConnectionPool != nil {
			extra = append(extra, "connection pool")
		}
		if trafficPolicy.OutlierDetection != nil {
			extra = append(extra, "outlier detection")
		}
		if trafficPolicy.PortLevelSettings != nil {
			extra = append(extra, "port level settings")
		}
		if len(extra) > 0 {
			fmt.Fprintf(writer, "   %s\n", strings.Join(extra, "/"))
		}
	}
}

// httpRouteMatchSvc returns true if it matches and a slice of facts about the match
func httpRouteMatchSvc(vs clientnetworking.VirtualService, route *v1alpha3.HTTPRoute, svc v1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *clientnetworking.DestinationRule) (bool, []string) { // nolint: lll
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	facts := []string{}
	mismatchNotes := []string{}
	match := false
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, model.ConfigMeta{Namespace: vs.Namespace}))
		if extendFQDN(fqdn) == svcHost {
			if dest.Destination.Subset != "" {
				if contains(nonmatchingSubsets, dest.Destination.Subset) {
					mismatchNotes = append(mismatchNotes, fmt.Sprintf("Route to non-matching subset %s for (%s)",
						dest.Destination.Subset,
						renderMatches(route.Match)))
					continue
				}
				if !contains(matchingSubsets, dest.Destination.Subset) {
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
				facts = append(facts, fmt.Sprintf("Weight %d%%", dest.Weight))
			}
			// Consider adding RemoveResponseHeaders, AppendResponseHeaders, RemoveRequestHeaders, AppendRequestHeaders
		} else {
			mismatchNotes = append(mismatchNotes, fmt.Sprintf("Route to %s", dest.Destination.Host))
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

func tcpRouteMatchSvc(vs clientnetworking.VirtualService, route *v1alpha3.TCPRoute, svc v1.Service) (bool, []string) {
	match := false
	facts := []string{}
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, model.ConfigMeta{Namespace: vs.Namespace}))
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
	retval := ""
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

func printPod(writer io.Writer, pod *v1.Pod) {
	ports := []string{}
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			var protocol string
			// Suppress /<protocol> for TCP, print it for everything else
			if port.Protocol != "TCP" {
				protocol = fmt.Sprintf("/%s", port.Protocol)
			}
			ports = append(ports, fmt.Sprintf("%d%s (%s)", port.ContainerPort, protocol, container.Name))
		}
	}

	fmt.Fprintf(writer, "Pod: %s\n", kname(pod.ObjectMeta))
	if len(ports) > 0 {
		fmt.Fprintf(writer, "   Pod Ports: %s\n", strings.Join(ports, ", "))
	} else {
		fmt.Fprintf(writer, "   Pod does not expose ports\n")
	}

	if pod.Status.Phase != v1.PodRunning {
		fmt.Printf("   Pod is not %s (%s)\n", v1.PodRunning, pod.Status.Phase)
		return
	}

	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			fmt.Fprintf(writer, "WARNING: Pod %s Container %s NOT READY\n", kname(pod.ObjectMeta), containerStatus.Name)
		}
	}

	if ignoreUnmeshed {
		return
	}

	if !isMeshed(pod) {
		fmt.Fprintf(writer, "WARNING: %s is not part of mesh; no Istio sidecar\n", kname(pod.ObjectMeta))
		return
	}

	// https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
	// says "We recommend adding an explicit app label and version label to deployments."
	app, ok := pod.ObjectMeta.Labels["app"]
	if !ok || app == "" {
		fmt.Fprintf(writer, "Suggestion: add 'app' label to pod for Istio telemetry.\n")
	}
	version, ok := pod.ObjectMeta.Labels["version"]
	if !ok || version == "" {
		fmt.Fprintf(writer, "Suggestion: add 'version' label to pod for Istio telemetry.\n")
	}
}

func kname(meta metav1.ObjectMeta) string {
	ns := handlers.HandleNamespace(namespace, defaultNamespace)
	if meta.Namespace == ns {
		return meta.Name
	}

	// Use the Istio convention pod-name[.namespace]
	return fmt.Sprintf("%s.%s", meta.Name, meta.Namespace)
}

func printService(writer io.Writer, svc v1.Service, pod *v1.Pod) {
	fmt.Fprintf(writer, "Service: %s\n", kname(svc.ObjectMeta))
	for _, port := range svc.Spec.Ports {
		if port.Protocol != "TCP" {
			// Ignore UDP ports, which are not supported by Istio
			continue
		}
		// Get port number
		nport, err := pilotcontroller.FindPort(pod, &port)
		if err == nil {
			var protocol string
			if port.Name == "" {
				protocol = "auto-detect"
			} else {
				protocol = string(servicePortProtocol(port.Name))
			}

			fmt.Fprintf(writer, "   Port: %s %d/%s targets pod port %d\n", port.Name, port.Port, protocol, nport)
		}
		msgs := validatePort(port, pod)
		for _, msg := range msgs {
			fmt.Fprintf(writer, "   %s\n", msg)
		}
	}
}

func contains(slice []string, s string) bool {
	for _, candidate := range slice {
		if candidate == s {
			return true
		}
	}

	return false
}

func isMeshed(pod *v1.Pod) bool {
	var sidecar bool

	for _, container := range pod.Spec.Containers {
		sidecar = sidecar || (container.Name == inject.ProxyContainerName)
	}

	return sidecar
}

// Extract value of key out of Struct, but always return a Struct, even if the value isn't one
func (v *myProtoValue) keyAsStruct(key string) *myProtoValue {
	if v == nil || v.GetStructValue() == nil {
		return asMyProtoValue(&structpb.Struct{Fields: make(map[string]*structpb.Value)})
	}

	return &myProtoValue{v.GetStructValue().Fields[key]}
}

// asMyProtoValue wraps a gogo Struct so we may use it with keyAsStruct and keyAsString
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
		if httpFilter.Name == authz_model.RBACHTTPFilterName {
			rbac := &rbac_http_filter.RBAC{}
			if err := ptypes.UnmarshalAny(httpFilter.GetTypedConfig(), rbac); err == nil {
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
func getInboundHTTPConnectionManager(cd *configdump.Wrapper, port int32) (*http_conn.HttpConnectionManager, error) {
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
		err = ptypes.UnmarshalAny(l.ActiveState.Listener, listenerTyped)
		if err != nil {
			return nil, err
		}
		if listenerTyped.Name == pilot_v1alpha3.VirtualInboundListenerName {
			for _, filterChain := range listenerTyped.FilterChains {
				for _, filter := range filterChain.Filters {
					hcm := &http_conn.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcm); err == nil {
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
					hcm := &http_conn.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcm); err == nil {
						return hcm, nil
					}
				}
			}
		}
	}

	return nil, nil
}

// getIstioConfigNameForSvc returns name, namespace
func getIstioVirtualServiceNameForSvc(cd *configdump.Wrapper, svc v1.Service, port int32) (string, string, error) {
	path, err := getIstioVirtualServicePathForSvcFromRoute(cd, svc, port)
	if err != nil {
		return "", "", err
	}

	// Starting with recent 1.5.0 builds, the path will include .istio.io.  Handle both.
	// nolint: gosimple
	re := regexp.MustCompile("/apis/networking(\\.istio\\.io)?/v1alpha3/namespaces/(?P<namespace>[^/]+)/virtual-service/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a VS path: %s", path)
	}
	return ss[3], ss[2], nil
}

// getIstioVirtualServicePathForSvcFromRoute returns something like "/apis/networking/v1alpha3/namespaces/default/virtual-service/reviews"
func getIstioVirtualServicePathForSvcFromRoute(cd *configdump.Wrapper, svc v1.Service, port int32) (string, error) {
	sPort := strconv.Itoa(int(port))

	// Routes know their destination Service name, namespace, and port, and the DR that configures them
	rcd, err := cd.GetDynamicRouteDump(false)
	if err != nil {
		return "", err
	}
	for _, rcd := range rcd.DynamicRouteConfigs {
		routeTyped := &route.RouteConfiguration{}
		err = ptypes.UnmarshalAny(rcd.RouteConfig, routeTyped)
		if err != nil {
			return "", err
		}
		if routeTyped.Name != sPort && !strings.HasPrefix(routeTyped.Name, "http.") {
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

func mixerConfigMatches(ns string, name string, tmixer *any.Any) bool {
	if tmixer != nil {
		svcName, svcNamespace, err := getTypedMixerDestinationSvc(tmixer)
		if err == nil && svcNamespace == ns && svcName == name {
			return true
		}
	}

	return false
}

// routeDestinationMatchesSvc determines if there ismixer configuration to use this service as a destination
func routeDestinationMatchesSvc(route *route.Route, svc v1.Service, vh *route.VirtualHost, port int32) bool {
	if route == nil {
		return false
	}

	// If Istio was deployed with telemetry or policy we'll have the K8s Service
	// nicely connected to the Envoy Route.
	// nolint: staticcheck
	if mixerConfigMatches(svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, route.GetTypedPerFilterConfig()["mixer"]) {
		return true
	}

	if rte := route.GetRoute(); rte != nil {
		if weightedClusters := rte.GetWeightedClusters(); weightedClusters != nil {
			for _, weightedCluster := range weightedClusters.Clusters {
				// nolint: staticcheck
				if mixerConfigMatches(svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, weightedCluster.GetTypedPerFilterConfig()["mixer"]) {
					return true
				}
			}
		}
	}

	// No mixer config, infer from VirtualHost domains matching <service>.<namespace>.svc.cluster.local
	re := regexp.MustCompile(`(?P<service>[^\.]+)\.(?P<namespace>[^\.]+)\.svc\.cluster\.local$`)
	for _, domain := range vh.Domains {
		ss := re.FindStringSubmatch(domain)
		if ss != nil {
			if ss[1] == svc.ObjectMeta.Name && ss[2] == svc.ObjectMeta.Namespace {
				return true
			}
		}
	}

	// If this is an ingress gateway, the Domains will be something like *:80, so check routes
	// which will look like "outbound|9080||productpage.default.svc.cluster.local"
	res := fmt.Sprintf(`outbound\|%d\|[^\|]*\|(?P<service>[^\.]+)\.(?P<namespace>[^\.]+)\.svc\.cluster\.local$`, port)
	re = regexp.MustCompile(res)
	ss := re.FindStringSubmatch(route.GetRoute().GetCluster())
	if ss != nil {
		if ss[1] == svc.ObjectMeta.Name && ss[2] == svc.ObjectMeta.Namespace {
			return true
		}
	}

	return false
}

func getTypedMixerDestinationSvc(tmixer *any.Any) (string, string, error) {
	serviceCfg := &mixerclient.ServiceConfig{}
	if err := ptypes.UnmarshalAny(tmixer, serviceCfg); err != nil {
		return "", "", err
	}
	svcNameValue, ok1 := serviceCfg.MixerAttributes.Attributes["destination.service.name"]
	svcNamespaceValue, ok2 := serviceCfg.MixerAttributes.Attributes["destination.service.namespace"]
	if ok1 && ok2 {
		return svcNameValue.GetStringValue(), svcNamespaceValue.GetStringValue(), nil
	}
	return "", "", fmt.Errorf("no mixer config")
}

// getIstioConfig returns .metadata.filter_metadata.istio.config, err
func getIstioConfig(metadata *envoy_api_core.Metadata) (string, error) {
	if metadata != nil {
		istioConfig := asMyProtoValue(metadata.FilterMetadata[util.IstioMetadataKey]).
			keyAsString("config")
		return istioConfig, nil
	}
	return "", fmt.Errorf("no istio config")
}

// getIstioConfigNameForSvc returns name, namespace
func getIstioDestinationRuleNameForSvc(cd *configdump.Wrapper, svc v1.Service, port int32) (string, string, error) {
	path, err := getIstioDestinationRulePathForSvc(cd, svc, port)
	if err != nil || path == "" {
		return "", "", err
	}

	// Starting with recent 1.5.0 builds, the path will include .istio.io.  Handle both.
	// nolint: gosimple
	re := regexp.MustCompile("/apis/networking(\\.istio\\.io)?/v1alpha3/namespaces/(?P<namespace>[^/]+)/destination-rule/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a DR path: %s", path)
	}
	return ss[3], ss[2], nil
}

// getIstioDestinationRulePathForSvc returns something like "/apis/networking/v1alpha3/namespaces/default/destination-rule/reviews"
func getIstioDestinationRulePathForSvc(cd *configdump.Wrapper, svc v1.Service, port int32) (string, error) {

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
		err = ptypes.UnmarshalAny(dac.Cluster, clusterTyped)
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
func printVirtualService(writer io.Writer, vs clientnetworking.VirtualService, svc v1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *clientnetworking.DestinationRule) { //nolint: lll
	fmt.Fprintf(writer, "VirtualService: %s\n", kname(vs.ObjectMeta))

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
				fmt.Fprintf(writer, "   %s\n", newfact)
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
				fmt.Fprintf(writer, "   %s\n", newfact)
				facts++
			}
		} else {
			mismatchNotes = append(mismatchNotes, newfacts...)
		}
	}

	if matches == 0 {
		if len(vs.Spec.Http) > 0 {
			fmt.Fprintf(writer, "   WARNING: No destinations match pod subsets (checked %d HTTP routes)\n", len(vs.Spec.Http))
		}
		if len(vs.Spec.Tcp) > 0 {
			fmt.Fprintf(writer, "   WARNING: No destinations match pod subsets (checked %d TCP routes)\n", len(vs.Spec.Tcp))
		}
		for _, mismatch := range mismatchNotes {
			fmt.Fprintf(writer, "      %s\n", mismatch)
		}
		return
	}

	possibleDests := len(vs.Spec.Http) + len(vs.Spec.Tls) + len(vs.Spec.Tcp)
	if matches < possibleDests {
		// We've printed the match conditions.  We can't say for sure that matching
		// traffic will reach this pod, because an earlier match condition could have captured it.
		fmt.Fprintf(writer, "   %d additional destination(s) that will not reach this pod\n", possibleDests-matches)
		// If we matched, but printed nothing, treat this as the catch-all
		if facts == 0 {
			for _, mismatch := range mismatchNotes {
				fmt.Fprintf(writer, "      %s\n", mismatch)
			}
		}

		return
	}

	if facts == 0 {
		// We printed nothing other than the name.  Print something.
		if len(vs.Spec.Http) > 0 {
			fmt.Fprintf(writer, "   %d HTTP route(s)\n", len(vs.Spec.Http))
		}
		if len(vs.Spec.Tcp) > 0 {
			fmt.Fprintf(writer, "   %d TCP route(s)\n", len(vs.Spec.Tcp))
		}
	}
}

func printIngressInfo(writer io.Writer, matchingServices []v1.Service, podsLabels []k8s_labels.Set, kubeClient kubernetes.Interface, configClient istioclient.Interface, client kube.ExtendedClient) error { // nolint: lll

	pods, err := kubeClient.CoreV1().Pods(istioNamespace).List(context.TODO(), metav1.ListOptions{
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
	pod := pods.Items[0]

	// Currently no support for non-standard gateways selecting non ingressgateway pods
	ingressSvcs, err := kubeClient.CoreV1().Services(istioNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "istio=ingressgateway",
	})
	if err != nil {
		return multierror.Prefix(err, "Could not find ingress gateway service")
	}
	if len(ingressSvcs.Items) == 0 {
		return fmt.Errorf("no ingress gateway service")
	}
	byConfigDump, err := client.EnvoyDo(context.TODO(), pod.Name, pod.Namespace, "GET", "config_dump", nil)
	if err != nil {
		return fmt.Errorf("failed to execute command on ingress gateway sidecar: %v", err)
	}

	cd := configdump.Wrapper{}
	err = cd.UnmarshalJSON(byConfigDump)
	if err != nil {
		return fmt.Errorf("can't parse ingress gateway sidecar config_dump: %v", err)
	}

	ipIngress := getIngressIP(ingressSvcs.Items[0], pod)

	for row, svc := range matchingServices {
		for _, port := range svc.Spec.Ports {
			matchingSubsets := []string{}
			nonmatchingSubsets := []string{}
			drName, drNamespace, err := getIstioDestinationRuleNameForSvc(&cd, svc, port.Port)
			var dr *clientnetworking.DestinationRule
			if err == nil && drName != "" && drNamespace != "" {
				dr, _ = configClient.NetworkingV1alpha3().DestinationRules(drNamespace).Get(context.Background(), drName, metav1.GetOptions{})
				if dr != nil {
					matchingSubsets, nonmatchingSubsets = getDestRuleSubsets(dr.Spec.Subsets, podsLabels)
				} else {
					fmt.Fprintf(writer,
						"WARNING: Proxy is stale; it references to non-existent destination rule %s.%s\n",
						drName, drNamespace)
				}
			}

			vsName, vsNamespace, err := getIstioVirtualServiceNameForSvc(&cd, svc, port.Port)
			if err == nil && vsName != "" && vsNamespace != "" {
				vs, _ := configClient.NetworkingV1alpha3().VirtualServices(vsNamespace).Get(context.Background(), vsName, metav1.GetOptions{})
				if vs != nil {
					if row == 0 {
						fmt.Fprintf(writer, "\n")
					} else {
						fmt.Fprintf(writer, "--------------------\n")
					}

					printIngressService(writer, &ingressSvcs.Items[0], &pod, ipIngress)
					printVirtualService(writer, *vs, svc, matchingSubsets, nonmatchingSubsets, dr)
				} else {
					fmt.Fprintf(writer,
						"WARNING: Proxy is stale; it references to non-existent virtual service %s.%s\n",
						vsName, vsNamespace)
				}
			}
		}
	}

	return nil
}

func printIngressService(writer io.Writer, ingressSvc *v1.Service, ingressPod *v1.Pod, ip string) {
	// The ingressgateway service offers a lot of ports but the pod doesn't listen to all
	// of them.  For example, it doesn't listen on 443 without additional setup.  This prints
	// the most basic output.
	portsToShow := map[string]bool{
		"http2": true,
	}
	protocolToScheme := map[string]string{
		"HTTP2": "http",
	}
	schemePortDefault := map[string]int{
		"http": 80,
	}

	for _, port := range ingressSvc.Spec.Ports {
		if port.Protocol != "TCP" || !portsToShow[port.Name] {
			continue
		}

		// Get port number
		_, err := pilotcontroller.FindPort(ingressPod, &port)
		if err == nil {
			nport := int(port.Port)
			protocol := string(servicePortProtocol(port.Name))

			scheme := protocolToScheme[protocol]
			portSuffix := ""
			if schemePortDefault[scheme] != nport {
				portSuffix = fmt.Sprintf(":%d", nport)
			}
			fmt.Fprintf(writer, "\nExposed on Ingress Gateway %s://%s%s\n", scheme, ip, portSuffix)
		}
	}
}

func getIngressIP(service v1.Service, pod v1.Pod) string {
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		return service.Status.LoadBalancer.Ingress[0].IP
	}

	if pod.Status.HostIP != "" {
		return pod.Status.HostIP
	}

	// The scope of this function is to get the IP from Kubernetes, we do not
	// ask Docker or Minikube for an IP.
	// See https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/#determining-the-ingress-ip-and-ports

	return "unknown"
}

func svcDescribeCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "service <svc>",
		Aliases: []string{"svc"},
		Short:   "Describe services and their Istio configuration [kube-only]",
		Long: `Analyzes service, pods, DestinationRules, and VirtualServices and reports
the configuration objects that affect that service.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `istioctl experimental describe service productpage`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("expecting service name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			svcName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))

			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			svc, err := client.CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()

			pods, err := client.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			matchingPods := []v1.Pod{}
			selectedPodCount := 0
			if len(svc.Spec.Selector) > 0 {
				svcSelector := k8s_labels.SelectorFromSet(svc.Spec.Selector)
				for _, pod := range pods.Items {
					if svcSelector.Matches(k8s_labels.Set(pod.ObjectMeta.Labels)) {
						selectedPodCount++

						if pod.Status.Phase != v1.PodRunning {
							fmt.Printf("   Pod is not %s (%s)\n", v1.PodRunning, pod.Status.Phase)
							continue
						}

						ready, err := containerReady(&pod, proxyContainerName)
						if err != nil {
							fmt.Fprintf(writer, "Pod %s: %s\n", kname(pod.ObjectMeta), err)
							continue
						}
						if !ready {
							fmt.Fprintf(writer, "WARNING: Pod %s Container %s NOT READY\n", kname(pod.ObjectMeta), proxyContainerName)
							continue
						}

						matchingPods = append(matchingPods, pod)
					}
				}
			}

			if len(matchingPods) == 0 {
				if selectedPodCount == 0 {
					fmt.Fprintf(writer, "Service %q has no pods.\n", kname(svc.ObjectMeta))
					return nil
				}
				fmt.Fprintf(writer, "Service %q has no Istio pods.  (%d pods in service).\n", kname(svc.ObjectMeta), selectedPodCount)
				fmt.Fprintf(writer, "Use `istioctl experimental add-to-mesh`, `istioctl kube-inject`, or redeploy with Istio automatic sidecar injection.\n")
				return nil
			}

			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}

			var configClient istioclient.Interface
			if configClient, err = configStoreFactory(); err != nil {
				return err
			}

			// Get all the labels for all the matching pods.  We will used this to complain
			// if NONE of the pods match a VirtaulService
			podsLabels := make([]k8s_labels.Set, len(matchingPods))
			for i, pod := range matchingPods {
				podsLabels[i] = k8s_labels.Set(pod.ObjectMeta.Labels)
			}

			// Describe based on the Envoy config for this first pod only
			pod := matchingPods[0]

			// Only consider the service invoked with this command, not other services that might select the pod
			svcs := []v1.Service{*svc}

			err = describePodServices(writer, kubeClient, configClient, &pod, svcs, podsLabels)
			if err != nil {
				return err
			}

			// Now look for ingress gateways
			return printIngressInfo(writer, svcs, podsLabels, client, configClient, kubeClient)
		},
	}

	cmd.PersistentFlags().BoolVar(&ignoreUnmeshed, "ignoreUnmeshed", false,
		"Suppress warnings for unmeshed pods")

	return cmd
}

func describePodServices(writer io.Writer, kubeClient kube.ExtendedClient, configClient istioclient.Interface, pod *v1.Pod, matchingServices []v1.Service, podsLabels []k8s_labels.Set) error { // nolint: lll
	var err error

	byConfigDump, err := kubeClient.EnvoyDo(context.TODO(), pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, "GET", "config_dump", nil)
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

		for _, port := range svc.Spec.Ports {
			matchingSubsets := []string{}
			nonmatchingSubsets := []string{}
			drName, drNamespace, err := getIstioDestinationRuleNameForSvc(&cd, svc, port.Port)
			if err != nil {
				log.Errorf("fetch destination rule for %v: %v", svc.Name, err)
			}
			var dr *clientnetworking.DestinationRule
			if err == nil && drName != "" && drNamespace != "" {
				dr, _ = configClient.NetworkingV1alpha3().DestinationRules(drNamespace).Get(context.Background(), drName, metav1.GetOptions{})
				if dr != nil {
					if len(svc.Spec.Ports) > 1 {
						// If there is more than one port, prefix each DR by the port it applies to
						fmt.Fprintf(writer, "%d ", port.Port)
					}
					printDestinationRule(writer, dr, podsLabels)
					matchingSubsets, nonmatchingSubsets = getDestRuleSubsets(dr.Spec.Subsets, podsLabels)
				} else {
					fmt.Fprintf(writer,
						"WARNING: Proxy is stale; it references to non-existent destination rule %s.%s\n",
						drName, drNamespace)
				}
			}

			vsName, vsNamespace, err := getIstioVirtualServiceNameForSvc(&cd, svc, port.Port)
			if err == nil && vsName != "" && vsNamespace != "" {
				vs, _ := configClient.NetworkingV1alpha3().VirtualServices(vsNamespace).Get(context.Background(), vsName, metav1.GetOptions{})
				if vs != nil {
					if len(svc.Spec.Ports) > 1 {
						// If there is more than one port, prefix each DR by the port it applies to
						fmt.Fprintf(writer, "%d ", port.Port)
					}
					printVirtualService(writer, *vs, svc, matchingSubsets, nonmatchingSubsets, dr)
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

func containerReady(pod *v1.Pod, containerName string) (bool, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.Ready, nil
		}
	}
	return false, fmt.Errorf("no container %q in pod", containerName)
}

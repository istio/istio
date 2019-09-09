// Copyright 2019 Istio Authors
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
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	envoy_api_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoy_api_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	rbac_http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	gogo_types "github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	istio_envoy_configdump "istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	"istio.io/istio/pilot/pkg/model"
	envoy_v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	pilotcontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/kube/inject"
)

type myGogoValue struct {
	*gogo_types.Value
}

const (
	k8sSuffix = ".svc.cluster.local"
)

var (
	// Function that creates Kubernetes client-go; making it a variable lets us mock client-go
	interfaceFactory = createInterface

	// Ignore unmeshed pods.  This makes it easy to suppress warnings about kube-system etc
	ignoreUnmeshed = false
)

func podDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pod",
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
			pod, err := client.CoreV1().Pods(ns).Get(podName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()

			podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)

			printPod(writer, pod)

			svcs, err := client.CoreV1().Services(ns).List(metav1.ListOptions{})
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

			kubeClient, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			var authnDebug *[]envoy_v2.AuthenticationDebug
			if isMeshed(pod) {
				// Use the mechanism of "istioctl authn tls-check" to look for CONFLICT
				// for this pod and show it.
				authnDebug, err = getAuthenticationz(kubeClient, podName, ns)
				if err != nil {
					// Keep going on error
					fmt.Fprintf(writer, "%s", err)
				}
			}

			byConfigDump, err := kubeClient.EnvoyDo(podName, ns, "GET", "config_dump", nil)
			if err != nil {
				if ignoreUnmeshed {
					return nil
				}

				return fmt.Errorf("failed to execute command on sidecar: %v", err)
			}

			cd := configdump.Wrapper{}
			err = cd.UnmarshalJSON(byConfigDump)
			if err != nil {
				return fmt.Errorf("can't parse sidecar config_dump: %v", err)
			}

			var configClient model.ConfigStore
			if configClient, err = clientFactory(); err != nil {
				return err
			}

			for _, svc := range matchingServices {
				fmt.Fprintf(writer, "--------------------\n")
				printService(writer, svc, pod)

				for _, port := range svc.Spec.Ports {
					matchingSubsets := []string{}
					nonmatchingSubsets := []string{}
					drName, drNamespace, err := getIstioDestinationRuleNameForSvc(&cd, svc, port.Port)
					var dr *model.Config
					if err == nil && drName != "" && drNamespace != "" {
						dr = configClient.Get(schemas.DestinationRule.Type, drName, drNamespace)
						if dr != nil {
							if len(svc.Spec.Ports) > 1 {
								// If there is more than one port, prefix each DR by the port it applies to
								fmt.Fprintf(writer, "%d ", port.Port)
							}
							printDestinationRule(writer, *dr, podLabels)
							matchingSubsets, nonmatchingSubsets = getDestRuleSubsets(*dr, podLabels)
						}
					}

					if len(svc.Spec.Ports) > 1 {
						// If there is more than one port, prefix each DR by the port it applies to
						fmt.Fprintf(writer, "%d ", port.Port)
					}
					printAuthnFromAuthenticationz(writer, authnDebug, pod, svc, port)

					vsName, vsNamespace, err := getIstioVirtualServiceNameForSvc(&cd, svc, port.Port)
					if err == nil && vsName != "" && vsNamespace != "" {
						vs := configClient.Get(schemas.VirtualService.Type, vsName, vsNamespace)
						if vs != nil {
							if len(svc.Spec.Ports) > 1 {
								// If there is more than one port, prefix each DR by the port it applies to
								fmt.Fprintf(writer, "%d ", port.Port)
							}
							printVirtualService(writer, *vs, svc, matchingSubsets, nonmatchingSubsets, dr)
						}
					}

					policies, _ := getIstioRBACPolicies(&cd, port.Port)
					if len(policies) > 0 {
						if len(svc.Spec.Ports) > 1 {
							// If there is more than one port, prefix each DR by the port it applies to
							fmt.Fprintf(writer, "%d ", port.Port)
						}

						fmt.Fprintf(writer, "RBAC policies: %s\n", strings.Join(policies, ", "))
					}
				}
			}

			// TODO find sidecar configs that select this workload and render them

			return nil
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
	nport, err := pilotcontroller.FindPort(pod, &port)
	if err != nil {
		retval = append(retval, err.Error())
	} else {
		_, ok := containerPorts[nport]
		if !ok {
			retval = append(retval,
				fmt.Sprintf("Warning: Pod %s port %d not exposed by Container", kname(pod.ObjectMeta), nport))
		}
	}

	if servicePortProtocol(port.Name) == protocol.Unsupported {
		retval = append(retval,
			fmt.Sprintf("%s is named %q which does not follow Istio conventions", port.TargetPort.String(), port.Name))
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

func svcFQDN(svc v1.Service) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace)
}

// getDestRuleSubsets gets names of subsets that match the pod labels (also, ones that don't match)
func getDestRuleSubsets(destRule model.Config, podLabels k8s_labels.Set) ([]string, []string) {
	drSpec, ok := destRule.Spec.(*v1alpha3.DestinationRule)
	if !ok {
		return []string{}, []string{}
	}

	matchingSubsets := []string{}
	nonmatchingSubsets := []string{}
	for _, subset := range drSpec.Subsets {
		subsetSelector := k8s_labels.SelectorFromSet(subset.Labels)
		if subsetSelector.Matches(podLabels) {
			matchingSubsets = append(matchingSubsets, subset.Name)
		} else {
			nonmatchingSubsets = append(nonmatchingSubsets, subset.Name)
		}
	}

	return matchingSubsets, nonmatchingSubsets
}

func printDestinationRule(writer io.Writer, destRule model.Config, podLabels k8s_labels.Set) {
	drSpec, ok := destRule.Spec.(*v1alpha3.DestinationRule)
	if !ok {
		return
	}

	fmt.Fprintf(writer, "DestinationRule: %s for %q\n", name(destRule), drSpec.Host)

	matchingSubsets, nonmatchingSubsets := getDestRuleSubsets(destRule, podLabels)

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
	trafficPolicy := drSpec.TrafficPolicy
	if trafficPolicy == nil {
		fmt.Fprintf(writer, "   No Traffic Policy\n")
	} else {
		if trafficPolicy.Tls != nil {
			fmt.Fprintf(writer, "   Traffic Policy TLS Mode: %s\n", drSpec.TrafficPolicy.Tls.Mode.String())
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
func httpRouteMatchSvc(virtualSvc model.Config, route *v1alpha3.HTTPRoute, svc v1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *model.Config) (bool, []string) { // nolint: lll
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	facts := []string{}
	mismatchNotes := []string{}
	match := false
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, virtualSvc.ConfigMeta))
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
						mismatchNotes = append(mismatchNotes, fmt.Sprintf("Warning: Route to UNKNOWN subset %s; check DestinationRule %s", dest.Destination.Subset, name(*dr)))
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

func tcpRouteMatchSvc(virtualSvc model.Config, route *v1alpha3.TCPRoute, svc v1.Service) (bool, []string) {
	match := false
	facts := []string{}
	svcHost := extendFQDN(fmt.Sprintf("%s.%s", svc.ObjectMeta.Name, svc.ObjectMeta.Namespace))
	for _, dest := range route.Route {
		fqdn := string(model.ResolveShortnameToFQDN(dest.Destination.Host, virtualSvc.ConfigMeta))
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
		fmt.Fprintf(writer, "WARNING: %s is part of mesh; no Istio sidecar\n", kname(pod.ObjectMeta))
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

func name(config model.Config) string {
	ns := handlers.HandleNamespace(namespace, defaultNamespace)
	if config.ConfigMeta.Namespace == ns {
		return config.ConfigMeta.Name
	}

	// Use the Istio convention pod-name[.namespace]
	return fmt.Sprintf("%s.%s", config.ConfigMeta.Name, config.ConfigMeta.Namespace)
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
			fmt.Fprintf(writer, "   Port: %s %d/%s\n", port.Name, nport, servicePortProtocol(port.Name))
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

func getAuthenticationz(kubeClient kubernetes.ExecClient, podName, ns string) (*[]envoy_v2.AuthenticationDebug, error) {
	results, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET",
		fmt.Sprintf("/debug/authenticationz?proxyID=%s.%s", podName, ns), nil)
	if err != nil {
		return nil, err
	}
	var debug []envoy_v2.AuthenticationDebug
	for i := range results {
		if err := json.Unmarshal(results[i], &debug); err == nil {
			if len(debug) > 0 {
				break
			}
		}
		// Ignore invalid responses from /debug/authenticationz
	}
	if len(debug) == 0 {
		return nil, fmt.Errorf("checked %d pilot instances and found no authentication info for %s.%s, check proxy status",
			len(results), podName, ns)
	}

	return &debug, nil
}

func authnMatchSvc(debug envoy_v2.AuthenticationDebug, svc v1.Service, port v1.ServicePort) bool {
	return debug.Host == svcFQDN(svc) && debug.Port == int(port.Port)
}

func printAuthn(writer io.Writer, pod *v1.Pod, debug envoy_v2.AuthenticationDebug) {
	if debug.TLSConflictStatus != "OK" {
		fmt.Fprintf(writer, "WARNING Pilot predicts TLS Conflict on %s port %d (pod enforces %s, clients speak %s)\n",
			kname(pod.ObjectMeta),
			debug.Port, debug.ServerProtocol, debug.ClientProtocol)
		if debug.DestinationRuleName != "-" {
			fmt.Fprintf(writer, "  Check DestinationRule %s and AuthenticationPolicy %s\n", debug.DestinationRuleName, debug.AuthenticationPolicyName)
		} else {
			fmt.Fprintf(writer, "  There is no DestinationRule.  Check AuthenticationPolicy %s\n", debug.AuthenticationPolicyName)
		}
		return
	}

	mTLSType := map[string]string{
		"HTTP":        "HTTP",
		"mTLS":        "STRICT",
		"HTTP/mTLS":   "PERMISSIVE",
		"TLS":         "SIMPLE",
		"custom mTLS": "custom mTLS",
		"UNKNOWN":     "Unknown",
	}
	tlsType, ok := mTLSType[debug.ServerProtocol]
	if !ok {
		tlsType = debug.ServerProtocol
	}

	fmt.Fprintf(writer, "Pilot reports that pod is %s (enforces %s) and clients speak %s\n",
		tlsType, debug.ServerProtocol, debug.ClientProtocol)
}

func isMeshed(pod *v1.Pod) bool {
	var sidecar bool

	for _, container := range pod.Spec.Containers {
		sidecar = sidecar || (container.Name == inject.ProxyContainerName)
	}

	return sidecar
}

// Extract value of key out of Struct, but always return a Struct, even if the value isn't one
func (v *myGogoValue) keyAsStruct(key string) *myGogoValue {
	if v == nil || v.GetStructValue() == nil {
		return asMyGogoValue(&gogo_types.Struct{Fields: make(map[string]*gogo_types.Value)})
	}

	return &myGogoValue{v.GetStructValue().Fields[key]}
}

// asMyGogoValue wraps a gogo Struct so we may use it with keyAsStruct and keyAsString
func asMyGogoValue(s *gogo_types.Struct) *myGogoValue {
	return &myGogoValue{
		&gogo_types.Value{
			Kind: &gogo_types.Value_StructValue{
				StructValue: s,
			},
		},
	}
}

func (v *myGogoValue) keyAsString(key string) string {
	s := v.keyAsStruct(key)
	return s.GetStringValue()
}

func getIstioRBACPolicies(cd *configdump.Wrapper, port int32) ([]string, error) {
	hcm, err := getInboundHTTPConnectionManager(cd, port)
	if err != nil || hcm == nil {
		return []string{}, err
	}

	// Identify RBAC policies.  Currently there are no "breadcrumbs" so we only
	// return the policy names, not the ServiceRole and ServiceRoleBinding names.
	for _, httpFilter := range hcm.HttpFilters {
		if httpFilter.Name == authz_model.RBACHTTPFilterName {
			rbac := &rbac_http_filter.RBAC{}
			if err := gogo_types.UnmarshalAny(httpFilter.GetTypedConfig(), rbac); err == nil {
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

	for _, listener := range listeners.DynamicActiveListeners {
		if filter.Verify(listener.Listener) {
			sockAddr := listener.Listener.Address.GetSocketAddress()
			if sockAddr != nil {
				// Skip outbound listeners
				if sockAddr.Address == "0.0.0.0" {
					continue
				}
			}

			for _, filterChain := range listener.Listener.FilterChains {
				for _, filter := range filterChain.Filters {
					hcm := &http_conn.HttpConnectionManager{}
					if err := gogo_types.UnmarshalAny(filter.GetTypedConfig(), hcm); err == nil {
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

	if path == "" {
		path, err = getIstioVirtualServicePathForSvcFromListener(cd, svc, port)
		if err != nil {
			return "", "", err
		}
	}

	re := regexp.MustCompile("/apis/networking/v1alpha3/namespaces/(?P<namespace>[^/]+)/virtual-service/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a DR path: %s", path)
	}
	return ss[2], ss[1], nil
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
		if rcd.RouteConfig.Name != sPort {
			continue
		}

		for _, vh := range rcd.RouteConfig.VirtualHosts {
			for _, route := range vh.Routes {
				if routeDestinationMatchesSvc(route, svc, vh) {
					return getIstioConfig(route.Metadata)
				}
			}
		}
	}
	return "", nil
}

// routeDestinationMatchesSvc determines if there ismixer configuration to use this service as a destination
func routeDestinationMatchesSvc(route *envoy_api_route.Route, svc v1.Service, vh *envoy_api_route.VirtualHost) bool {
	if route == nil {
		return false
	}

	mixer, ok := route.PerFilterConfig["mixer"]
	if ok {
		svcName, svcNamespace, err := getMixerDestinationSvc(mixer)
		if err == nil && svcNamespace == svc.ObjectMeta.Namespace && svcName == svc.ObjectMeta.Name {
			return true
		}
	}

	if rte := route.GetRoute(); rte != nil {
		if weightedClusters := rte.GetWeightedClusters(); weightedClusters != nil {
			for _, weightedCluster := range weightedClusters.Clusters {
				mixer, ok := weightedCluster.PerFilterConfig["mixer"]
				if ok {
					svcName, svcNamespace, err := getMixerDestinationSvc(mixer)
					if err == nil && svcNamespace == svc.ObjectMeta.Namespace && svcName == svc.ObjectMeta.Name {
						return true
					}
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

	return false
}

// getMixerDestinationSvc returns name, namespace, err
func getMixerDestinationSvc(mixer *gogo_types.Struct) (string, string, error) {
	if mixer != nil {
		attributes := asMyGogoValue(mixer).
			keyAsStruct("mixer_attributes").
			keyAsStruct("attributes")
		svcName := attributes.keyAsStruct("destination.service.name").
			keyAsString("string_value")
		svcNamespace := attributes.keyAsStruct("destination.service.namespace").
			keyAsString("string_value")
		return svcName, svcNamespace, nil
	}
	return "", "", fmt.Errorf("no mixer config")
}

// getIstioConfig returns .metadata.filter_metadata.istio.config, err
func getIstioConfig(metadata *envoy_api_core.Metadata) (string, error) {
	if metadata != nil {
		istioConfig := asMyGogoValue(metadata.FilterMetadata["istio"]).
			keyAsString("config")
		return istioConfig, nil
	}
	return "", fmt.Errorf("no istio config")
}

// getIstioConfigNameForSvc returns name, namespace
func getIstioDestinationRuleNameForSvc(cd *configdump.Wrapper, svc v1.Service, port int32) (string, string, error) {
	path, err := getIstioDestinationRulePathForSvc(cd, svc, port)
	if err != nil {
		return "", "", err
	}

	re := regexp.MustCompile("/apis/networking/v1alpha3/namespaces/(?P<namespace>[^/]+)/destination-rule/(?P<name>[^/]+)")
	ss := re.FindStringSubmatch(path)
	if ss == nil {
		return "", "", fmt.Errorf("not a DR path: %s", path)
	}
	return ss[2], ss[1], nil
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
		cluster := dac.Cluster
		if filter.Verify(cluster) {
			metadata := cluster.Metadata
			if metadata != nil {
				istioConfig := asMyGogoValue(metadata.FilterMetadata["istio"]).
					keyAsString("config")
				return istioConfig, nil
			}
		}
	}

	return "", nil
}

// TODO simplify this by showing for each matching Destination the negation of the previous HttpMatchRequest
// and showing the non-matching Destinations.  (The current code is ad-hoc, and usually shows most of that information.)
func printVirtualService(writer io.Writer, virtualSvc model.Config, svc v1.Service, matchingSubsets []string, nonmatchingSubsets []string, dr *model.Config) {
	fmt.Fprintf(writer, "VirtualService: %s\n", name(virtualSvc))

	vsSpec, ok := virtualSvc.Spec.(*v1alpha3.VirtualService)
	if !ok {
		return
	}

	// There is no point in checking that 'port' uses HTTP (for HTTP route matches)
	// or uses TCP (for TCP route matches) because if the port has the wrong name
	// the VirtualService metadata will not appear.

	matches := 0
	facts := 0
	mismatchNotes := []string{}
	for _, httpRoute := range vsSpec.Http {
		routeMatch, newfacts := httpRouteMatchSvc(virtualSvc, httpRoute, svc, matchingSubsets, nonmatchingSubsets, dr)
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

	for _, tcpRoute := range vsSpec.Tcp {
		routeMatch, newfacts := tcpRouteMatchSvc(virtualSvc, tcpRoute, svc)
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
		if len(vsSpec.Http) > 0 {
			fmt.Fprintf(writer, "   WARNING: No destinations match pod subsets (checked %d HTTP routes)\n", len(vsSpec.Http))
		}
		if len(vsSpec.Tcp) > 0 {
			fmt.Fprintf(writer, "   WARNING: No destinations match pod subsets (checked %d TCP routes)\n", len(vsSpec.Tcp))
		}
		for _, mismatch := range mismatchNotes {
			fmt.Fprintf(writer, "      %s\n", mismatch)
		}
		return
	}

	possibleDests := len(vsSpec.Http) + len(vsSpec.Tls) + len(vsSpec.Tcp)
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
		if len(vsSpec.Http) > 0 {
			fmt.Fprintf(writer, "   %d HTTP route(s)\n", len(vsSpec.Http))
		}
		if len(vsSpec.Tcp) > 0 {
			fmt.Fprintf(writer, "   %d TCP route(s)\n", len(vsSpec.Tcp))
		}
	}

}

func printAuthnFromAuthenticationz(writer io.Writer, debug *[]envoy_v2.AuthenticationDebug, pod *v1.Pod, svc v1.Service, port v1.ServicePort) {
	if debug == nil {
		return
	}

	count := 0
	matchingAuthns := []envoy_v2.AuthenticationDebug{}
	for _, authn := range *debug {
		if authnMatchSvc(authn, svc, port) {
			matchingAuthns = append(matchingAuthns, authn)
		}
	}
	for _, matchingAuthn := range matchingAuthns {
		printAuthn(writer, pod, matchingAuthn)
		count++
	}

	if count == 0 {
		fmt.Fprintf(writer, "None\n")
	}
}

// getIstioVirtualServicePathForSvcFromListener returns something like "/apis/networking/v1alpha3/namespaces/default/virtual-service/reviews"
func getIstioVirtualServicePathForSvcFromListener(cd *configdump.Wrapper, svc v1.Service, port int32) (string, error) {

	filter := istio_envoy_configdump.ListenerFilter{
		Port: uint32(port),
		Type: "TCP",
	}
	listeners, err := cd.GetListenerConfigDump()
	if err != nil {
		return "", err
	}

	// VirtualServices for TCP may appear in the listeners
	for _, listener := range listeners.DynamicActiveListeners {
		if filter.Verify(listener.Listener) {
			for _, filterChain := range listener.Listener.FilterChains {
				for _, filter := range filterChain.Filters {
					if filter.Name == "mixer" {
						svcName, svcNamespace, err := getMixerDestinationSvc(filter.GetConfig())
						if err == nil && svcName == svc.ObjectMeta.Name && svcNamespace == svc.ObjectMeta.Namespace {
							return getIstioConfig(filterChain.Metadata)
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("listener has no VirtualService")
}

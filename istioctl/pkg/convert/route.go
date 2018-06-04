// Copyright 2017 Istio Authors.
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

package convert

import (
	"strings"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// RouteRules converts v1alpha1 route rules to v1alpha3 virtual services
func RouteRules(configs []model.Config) []model.Config {
	ruleConfigs := make([]model.Config, 0)
	for _, config := range configs {
		if config.Type == model.RouteRule.Type {
			ruleConfigs = append(ruleConfigs, config)
		}
	}

	model.SortRouteRules(ruleConfigs)

	routeRules := make([]*v1alpha1.RouteRule, 0)
	for _, ruleConfig := range ruleConfigs {
		routeRules = append(routeRules, ruleConfig.Spec.(*v1alpha1.RouteRule))
	}

	virtualServices := make(map[string]*v1alpha3.VirtualService) // host -> virtual service
	for _, routeRule := range routeRules {
		host := convertIstioService(routeRule.Destination)
		if virtualServices[host] == nil {
			virtualServices[host] = &v1alpha3.VirtualService{
				Hosts: []string{host},
			}
		}
		rule := virtualServices[host]

		rule.Http = append(rule.Http, convertRouteRule(routeRule))
	}

	out := make([]model.Config, 0)
	for host, virtualService := range virtualServices {
		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.VirtualService.Type,
				Name:      host,
				Namespace: configs[0].Namespace,
				Domain:    configs[0].Domain,
			},
			Spec: virtualService,
		})
	}

	return out
}

func convertRouteRule(in *v1alpha1.RouteRule) *v1alpha3.HTTPRoute {
	if in == nil {
		return nil
	}

	if in.L4Fault != nil {
		log.Warn("L4 fault not supported")
	}

	return &v1alpha3.HTTPRoute{
		Match:            convertMatch(in.Match),
		Route:            convertRoutes(in),
		Redirect:         convertRedirect(in.Redirect),
		Rewrite:          convertRewrite(in.Rewrite),
		WebsocketUpgrade: in.WebsocketUpgrade,
		Timeout:          convertHTTPTimeout(in.HttpReqTimeout),
		Retries:          convertRetry(in.HttpReqRetries),
		Fault:            convertHTTPFault(in.HttpFault),
		Mirror:           convertMirror(in.Mirror),
		CorsPolicy:       convertCORSPolicy(in.CorsPolicy),
		AppendHeaders:    in.AppendHeaders,
	}
}

func convertMatch(in *v1alpha1.MatchCondition) []*v1alpha3.HTTPMatchRequest {
	if in == nil {
		return nil
	}

	out := &v1alpha3.HTTPMatchRequest{
		Headers: make(map[string]*v1alpha3.StringMatch),
	}

	if in.Request != nil {
		for name, stringMatch := range in.Request.Headers {
			switch name {
			case model.HeaderMethod:
				out.Method = convertStringMatch(stringMatch)
			case model.HeaderAuthority:
				out.Authority = convertStringMatch(stringMatch)
			case model.HeaderScheme:
				out.Scheme = convertStringMatch(stringMatch)
			case model.HeaderURI:
				out.Uri = convertStringMatch(stringMatch)
			default:
				out.Headers[name] = convertStringMatch(stringMatch)
			}
		}
	}

	if in.Tcp != nil {
		log.Warn("TCP matching not supported")
	}
	if in.Udp != nil {
		log.Warn("UDP matching not supported")
	}
	if in.Source != nil {
		// TODO: out.SourceLabels
		log.Warn("Source matching not supported")
	}

	return []*v1alpha3.HTTPMatchRequest{out}
}

func convertStringMatch(in *v1alpha1.StringMatch) *v1alpha3.StringMatch {
	out := &v1alpha3.StringMatch{}
	switch m := in.MatchType.(type) {
	case *v1alpha1.StringMatch_Exact:
		out.MatchType = &v1alpha3.StringMatch_Exact{Exact: m.Exact}
	case *v1alpha1.StringMatch_Prefix:
		out.MatchType = &v1alpha3.StringMatch_Prefix{Prefix: m.Prefix}
	case *v1alpha1.StringMatch_Regex:
		out.MatchType = &v1alpha3.StringMatch_Regex{Regex: m.Regex}
	default:
		log.Warn("Unsupported string match type")
		return nil
	}
	return out
}

func convertRoutes(in *v1alpha1.RouteRule) []*v1alpha3.DestinationWeight {
	host := convertIstioService(in.Destination)

	var out []*v1alpha3.DestinationWeight
	if in.Redirect == nil && len(in.Route) == 0 {
		out = append(out, &v1alpha3.DestinationWeight{
			Destination: &v1alpha3.Destination{
				Host:   host,
				Subset: "",
				Port:   nil,
			},
			Weight: 100,
		})
	}

	for _, route := range in.Route {
		name := host
		if route.Destination != nil {
			name = convertIstioService(route.Destination)
		}

		out = append(out, &v1alpha3.DestinationWeight{
			Destination: &v1alpha3.Destination{
				Host:   name,
				Subset: labelsToSubsetName(route.Labels),
				Port:   nil,
			},
			Weight: route.Weight,
		})
	}

	return out
}

func convertRedirect(in *v1alpha1.HTTPRedirect) *v1alpha3.HTTPRedirect {
	if in == nil {
		return nil
	}

	out := v1alpha3.HTTPRedirect{
		Uri:       in.Uri,
		Authority: in.Authority,
	}

	return &out
}

func convertRewrite(in *v1alpha1.HTTPRewrite) *v1alpha3.HTTPRewrite {
	if in == nil {
		return nil
	}

	out := v1alpha3.HTTPRewrite{
		Uri:       in.Uri,
		Authority: in.Authority,
	}
	return &out
}

func convertHTTPTimeout(in *v1alpha1.HTTPTimeout) *types.Duration {
	if in == nil {
		return nil
	}

	if st := in.GetSimpleTimeout(); st != nil {
		if st.OverrideHeaderName != "" {
			log.Warn("Timeout override header name not supported, ignored")
		}

		return convertGogoDuration(st.Timeout)
	}

	if ct := in.GetCustom(); ct != nil {
		log.Warn("Custom timeout policy not supported")
	}

	return nil
}

func convertRetry(in *v1alpha1.HTTPRetry) *v1alpha3.HTTPRetry {
	if in == nil {
		return nil
	}

	switch v := in.RetryPolicy.(type) {
	case *v1alpha1.HTTPRetry_SimpleRetry:
		if v.SimpleRetry.OverrideHeaderName != "" {
			log.Warn("Simple retry override header name not supported")
		}

		return &v1alpha3.HTTPRetry{
			Attempts:      v.SimpleRetry.Attempts,
			PerTryTimeout: convertGogoDuration(v.SimpleRetry.PerTryTimeout),
		}
	case *v1alpha1.HTTPRetry_Custom:
		log.Warn("Custom retry not supported")
	}
	return nil
}

func convertHTTPFault(in *v1alpha1.HTTPFaultInjection) *v1alpha3.HTTPFaultInjection {
	if in == nil {
		return nil
	}

	return &v1alpha3.HTTPFaultInjection{
		Abort: convertFaultAbort(in.Abort),
		Delay: convertFaultDelay(in.Delay),
	}
}

func convertFaultAbort(in *v1alpha1.HTTPFaultInjection_Abort) *v1alpha3.HTTPFaultInjection_Abort {
	if in == nil {
		return nil
	}

	out := &v1alpha3.HTTPFaultInjection_Abort{
		Percent: convertPercent(in.Percent),
	}

	if in.OverrideHeaderName != "" {
		log.Warn("Abort override header name not supported")
	}

	switch v := in.ErrorType.(type) {
	case *v1alpha1.HTTPFaultInjection_Abort_HttpStatus:
		out.ErrorType = &v1alpha3.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: v.HttpStatus}
	case *v1alpha1.HTTPFaultInjection_Abort_GrpcStatus:
		out.ErrorType = &v1alpha3.HTTPFaultInjection_Abort_GrpcStatus{GrpcStatus: v.GrpcStatus}
	case *v1alpha1.HTTPFaultInjection_Abort_Http2Error:
		out.ErrorType = &v1alpha3.HTTPFaultInjection_Abort_Http2Error{Http2Error: v.Http2Error}
	}

	return out
}

func convertFaultDelay(in *v1alpha1.HTTPFaultInjection_Delay) *v1alpha3.HTTPFaultInjection_Delay {
	if in == nil {
		return nil
	}

	out := &v1alpha3.HTTPFaultInjection_Delay{
		Percent: convertPercent(in.Percent),
	}

	if in.OverrideHeaderName != "" {
		log.Warn("Delay override header name not supported")
	}

	switch v := in.HttpDelayType.(type) {
	case *v1alpha1.HTTPFaultInjection_Delay_ExponentialDelay:
		log.Warn("Exponential delay is not supported")
		return nil
	case *v1alpha1.HTTPFaultInjection_Delay_FixedDelay:
		out.HttpDelayType = &v1alpha3.HTTPFaultInjection_Delay_FixedDelay{
			FixedDelay: convertGogoDuration(v.FixedDelay)}
	}

	return out
}

func convertPercent(in float32) int32 {
	out := int32(in)
	if in != float32(out) {
		log.Warn("Percent truncated")
	}
	return out
}

func convertMirror(in *v1alpha1.IstioService) *v1alpha3.Destination {
	if in == nil {
		return nil
	}

	return &v1alpha3.Destination{
		Host:   convertIstioService(in),
		Subset: labelsToSubsetName(in.Labels),
	}
}

func convertCORSPolicy(in *v1alpha1.CorsPolicy) *v1alpha3.CorsPolicy {
	if in == nil {
		return nil
	}

	return &v1alpha3.CorsPolicy{
		AllowOrigin:      in.AllowOrigin,
		AllowMethods:     in.AllowMethods,
		AllowHeaders:     in.AllowHeaders,
		ExposeHeaders:    in.ExposeHeaders,
		MaxAge:           convertGogoDuration(in.MaxAge),
		AllowCredentials: &types.BoolValue{Value: in.AllowCredentials.Value},
	}
}

// TODO: domain, namespace etc?
// convert an Istio service to a v1alpha3 host
func convertIstioService(service *v1alpha1.IstioService) string {
	if service.Service != "" { // Favor FQDN
		return service.Service
	}
	return service.Name // otherwise shortname
}

// create an unique DNS1123 friendly string
func labelsToSubsetName(labels model.Labels) string {
	return strings.NewReplacer("=", "-",
		".", "-").Replace(labels.String())
}

func convertGogoDuration(in *duration.Duration) *types.Duration {
	return &types.Duration{
		Seconds: in.Seconds,
		Nanos:   in.Nanos,
	}
}

// RouteRuleRouteLabels converts v1alpha1 route rule route labels to v1alpha3 destination rules
func RouteRuleRouteLabels(generateds []model.Config, configs []model.Config) []model.Config {
	ruleConfigs := make([]model.Config, 0)
	for _, config := range configs {
		if config.Type == model.RouteRule.Type {
			ruleConfigs = append(ruleConfigs, config)
		}
	}

	model.SortRouteRules(ruleConfigs)

	routeRules := make([]*v1alpha1.RouteRule, 0)
	for _, ruleConfig := range ruleConfigs {
		routeRules = append(routeRules, ruleConfig.Spec.(*v1alpha1.RouteRule))
	}

	destinationRules := make(map[string]*v1alpha3.DestinationRule) // host -> destination rule

	// Populate with DestinationRules that have already been generated
	for _, generated := range generateds {
		if generated.Type == model.DestinationRule.Type {
			destinationRule := generated.Spec.(*v1alpha3.DestinationRule)
			destinationRules[destinationRule.Host] = destinationRule
		}
	}

	// Track rules for hosts we don't already have a DestinationRule for
	newDestinationRules := make(map[string]*v1alpha3.DestinationRule) // host -> destination rule

	for _, routeRule := range routeRules {
		host := convertIstioService(routeRule.Destination)
		rule, ok := destinationRules[host]
		if !ok {
			// There is no existing DestinationRule to merge with; create a new one
			rule = &v1alpha3.DestinationRule{
				Host:    host,
				Subsets: make([]*v1alpha3.Subset, 0),
			}

			destinationRules[host] = rule
			newDestinationRules[host] = rule
		}

		// Merge required Subsets with existing Subsets
		for _, subset := range convertRouteRuleLabels(routeRule) {
			found := false
			for _, candidate := range rule.Subsets {
				if candidate.Name == subset.Name {
					// A subset by this name already exists.  As the names are generated we expect labels to match
					found = true
					break
				}
			}

			if !found {
				// We did not find an existing subset with the expected name; use the Subset converted from the Route
				rule.Subsets = append(rule.Subsets, subset)
			}
		}
	}

	out := make([]model.Config, 0)
	for host, newDestinationRule := range newDestinationRules {
		if len(newDestinationRule.Subsets) > 0 {
			out = append(out, model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:      model.DestinationRule.Type,
					Name:      host,
					Namespace: configs[0].Namespace,
					Domain:    configs[0].Domain,
				},
				Spec: newDestinationRule,
			})
		}
	}

	return out
}

func convertRouteRuleLabels(in *v1alpha1.RouteRule) []*v1alpha3.Subset {

	subsets := make([]*v1alpha3.Subset, 0)

	for _, route := range in.Route {
		subsets = append(subsets, &v1alpha3.Subset{
			Name:   labelsToSubsetName(route.Labels),
			Labels: route.Labels,
		})
	}

	return subsets
}

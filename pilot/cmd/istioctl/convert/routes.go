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

	"github.com/golang/protobuf/ptypes/duration"

	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func convertRouteRules(configs []model.Config) []model.Config {
	ruleConfigs := make([]model.Config, 0)
	for _, config := range configs {
		if config.Type == model.RouteRule.Type {
			ruleConfigs = append(ruleConfigs, config)
		}
	}

	model.SortRouteRules(ruleConfigs)

	v1rules := make([]*routing.RouteRule, 0)
	for _, ruleConfig := range ruleConfigs {
		v1rules = append(v1rules, ruleConfig.Spec.(*routing.RouteRule))
	}

	ruleByHost := make(map[string]*routingv2.RouteRule)
	for _, v1rule := range v1rules {
		host := convertIstioService(v1rule.Destination)
		if ruleByHost[host] == nil {
			ruleByHost[host] = &routingv2.RouteRule{
				Hosts: []string{host},
			}
		}
		rule := ruleByHost[host]

		rule.Http = append(rule.Http, convertRouteRule(v1rule))
	}

	out := make([]model.Config, 0)
	for _, v2rule := range ruleByHost {
		// TODO: ConfigMeta needs to be aggregated from source configs somehow
		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:            model.V1alpha2RouteRule.Type,
				Name:            "FIXME",
				Namespace:       "FIXME",
				Domain:          "FIXME",
				Labels:          nil,
				Annotations:     nil,
				ResourceVersion: "FIXME",
			},
			Spec: v2rule,
		})
	}

	return out
}

func convertRouteRule(in *routing.RouteRule) *routingv2.HTTPRoute {
	if in == nil {
		return nil
	}

	if in.L4Fault != nil {
		log.Warn("L4 fault not supported")
	}

	return &routingv2.HTTPRoute{
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

func convertMatch(in *routing.MatchCondition) []*routingv2.HTTPMatchRequest {
	if in == nil {
		return nil
	}

	out := &routingv2.HTTPMatchRequest{
		Headers: make(map[string]*routingv2.StringMatch),
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

	return []*routingv2.HTTPMatchRequest{out}
}

func convertStringMatch(in *routing.StringMatch) *routingv2.StringMatch {
	out := &routingv2.StringMatch{}
	switch m := in.MatchType.(type) {
	case *routing.StringMatch_Exact:
		out.MatchType = &routingv2.StringMatch_Exact{Exact: m.Exact}
	case *routing.StringMatch_Prefix:
		out.MatchType = &routingv2.StringMatch_Prefix{Prefix: m.Prefix}
	case *routing.StringMatch_Regex:
		out.MatchType = &routingv2.StringMatch_Regex{Regex: m.Regex}
	default:
		log.Warn("Unsupported string match type")
		return nil
	}
	return out
}

func convertRoutes(in *routing.RouteRule) []*routingv2.DestinationWeight {
	host := convertIstioService(in.Destination)
	out := make([]*routingv2.DestinationWeight, 0)
	for _, route := range in.Route {
		name := host
		if route.Destination != nil {
			name = convertIstioService(route.Destination)
		}

		out = append(out, &routingv2.DestinationWeight{
			Destination: &routingv2.Destination{
				Name:   name,
				Subset: labelsToSubsetName(route.Labels),
				Port:   nil,
			},
			Weight: route.Weight,
		})
	}

	return out
}

func convertRedirect(in *routing.HTTPRedirect) *routingv2.HTTPRedirect {
	if in == nil {
		return nil
	}

	out := routingv2.HTTPRedirect(*in) // structs are identical
	return &out
}

func convertRewrite(in *routing.HTTPRewrite) *routingv2.HTTPRewrite {
	if in == nil {
		return nil
	}

	out := routingv2.HTTPRewrite(*in) // structs are identical
	return &out
}

func convertHTTPTimeout(in *routing.HTTPTimeout) *duration.Duration {
	if in == nil {
		return nil
	}

	if st := in.GetSimpleTimeout(); st != nil {
		if st.OverrideHeaderName != "" {
			log.Warn("Timeout override header name not supported, ignored")
		}

		return st.Timeout
	}

	if ct := in.GetCustom(); ct != nil {
		log.Warn("Custom timeout policy not supported")
	}

	return nil
}

func convertRetry(in *routing.HTTPRetry) *routingv2.HTTPRetry {
	if in == nil {
		return nil
	}

	switch v := in.RetryPolicy.(type) {
	case *routing.HTTPRetry_SimpleRetry:
		if v.SimpleRetry.OverrideHeaderName != "" {
			log.Warn("Simple retry override header name not supported")
		}

		return &routingv2.HTTPRetry{
			Attempts:      v.SimpleRetry.Attempts,
			PerTryTimeout: v.SimpleRetry.PerTryTimeout,
		}
	case *routing.HTTPRetry_Custom:
		log.Warn("Custom retry not supported")
	}
	return nil
}

func convertHTTPFault(in *routing.HTTPFaultInjection) *routingv2.HTTPFaultInjection {
	if in == nil {
		return nil
	}

	return &routingv2.HTTPFaultInjection{
		Abort: convertFaultAbort(in.Abort),
		Delay: convertFaultDelay(in.Delay),
	}
}

func convertFaultAbort(in *routing.HTTPFaultInjection_Abort) *routingv2.HTTPFaultInjection_Abort {
	if in == nil {
		return nil
	}

	out := &routingv2.HTTPFaultInjection_Abort{
		Percent: convertPercent(in.Percent),
	}

	if in.OverrideHeaderName != "" {
		log.Warn("Abort override header name not supported")
	}

	switch v := in.ErrorType.(type) {
	case *routing.HTTPFaultInjection_Abort_HttpStatus:
		out.ErrorType = &routingv2.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: v.HttpStatus}
	case *routing.HTTPFaultInjection_Abort_GrpcStatus:
		out.ErrorType = &routingv2.HTTPFaultInjection_Abort_GrpcStatus{GrpcStatus: v.GrpcStatus}
	case *routing.HTTPFaultInjection_Abort_Http2Error:
		out.ErrorType = &routingv2.HTTPFaultInjection_Abort_Http2Error{Http2Error: v.Http2Error}
	}

	return out
}

func convertFaultDelay(in *routing.HTTPFaultInjection_Delay) *routingv2.HTTPFaultInjection_Delay {
	if in == nil {
		return nil
	}

	out := &routingv2.HTTPFaultInjection_Delay{
		Percent: convertPercent(in.Percent),
	}

	if in.OverrideHeaderName != "" {
		log.Warn("Delay override header name not supported")
	}

	switch v := in.HttpDelayType.(type) {
	case *routing.HTTPFaultInjection_Delay_ExponentialDelay:
		log.Warn("Exponential delay is not supported")
		return nil
	case *routing.HTTPFaultInjection_Delay_FixedDelay:
		out.HttpDelayType = &routingv2.HTTPFaultInjection_Delay_FixedDelay{FixedDelay: v.FixedDelay}
	}

	return out
}

func convertPercent(in float32) int32 {
	out := int32(in * 100)
	if in != float32(out)/100 {
		log.Warn("Percent truncated")
	}
	return out
}

func convertMirror(in *routing.IstioService) *routingv2.Destination {
	if in == nil {
		return nil
	}

	return &routingv2.Destination{
		Name:   convertIstioService(in),
		Subset: labelsToSubsetName(in.Labels),
	}
}

func convertCORSPolicy(in *routing.CorsPolicy) *routingv2.CorsPolicy {
	if in == nil {
		return nil
	}

	out := routingv2.CorsPolicy(*in) // structs are identical
	return &out
}

// FIXME: domain, namespace etc?
// convert an Istio service to a v1alpha2 host
func convertIstioService(service *routing.IstioService) string {
	if service.Service != "" { // Favor FQDN
		return service.Service
	}
	return service.Name // otherwise shortname
}

// create an unique DNS1123 friendly string
func labelsToSubsetName(labels model.Labels) string {
	return strings.Replace(labels.String(), "=", "-", -1)
}

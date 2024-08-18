package mseingress

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	lrlhttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	types "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	any "google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/log"
)

const (
	DefaultLocalRateLimitStatPrefix = "http_local_rate_limiter"

	LocalRateLimitFilterName = "envoy.filters.http.local_ratelimit"
)

var (
	filterEnable = &core.RuntimeFractionalPercent{
		DefaultValue: &types.FractionalPercent{
			Denominator: types.FractionalPercent_HUNDRED,
			Numerator:   100,
		},
		RuntimeKey: "local_rate_limit_enabled",
	}

	filterEnforce = &core.RuntimeFractionalPercent{
		DefaultValue: &types.FractionalPercent{
			Denominator: types.FractionalPercent_HUNDRED,
			Numerator:   100,
		},
		RuntimeKey: "local_rate_limit_enforced",
	}

	responseHeadersToAdd = []*core.HeaderValueOption{
		{
			Append: &wrappers.BoolValue{
				Value: false,
			},
			Header: &core.HeaderValue{
				Key:   "x-local-rate-limit",
				Value: "true",
			},
		},
	}
)

func GetLocalRateLimitFilter(filter *http_conn.HttpFilter) *lrlhttppb.LocalRateLimit {
	localRateLimit := &lrlhttppb.LocalRateLimit{}
	switch c := filter.ConfigType.(type) {
	case *http_conn.HttpFilter_TypedConfig:
		if err := c.TypedConfig.UnmarshalTo(localRateLimit); err != nil {
			log.Debugf("failed to get localRateLimit config: %s", err)
			return nil
		}
	case *http_conn.HttpFilter_ConfigDiscovery:
		return nil
	}
	return localRateLimit
}

func AddLocalRateLimitFilter(perFilterConfig map[string]*any.Any, filter *networking.HTTPFilter) {
	config := filter.GetLocalRateLimit()
	if config == nil || config.TokenBucket == nil {
		return
	}

	localRateLimit := &lrlhttppb.LocalRateLimit{
		StatPrefix:           DefaultLocalRateLimitStatPrefix,
		FilterEnabled:        filterEnable,
		FilterEnforced:       filterEnforce,
		ResponseHeadersToAdd: responseHeadersToAdd,
		TokenBucket: &types.TokenBucket{
			MaxTokens: config.TokenBucket.MaxTokens,
			TokensPerFill: &wrappers.UInt32Value{
				Value: config.TokenBucket.TokensPefFill,
			},
			FillInterval: &duration.Duration{
				Seconds: config.TokenBucket.FillInterval.GetSeconds(),
			},
		},
		Status: &types.HttpStatus{
			Code: types.StatusCode(config.StatusCode),
		},
	}

	perFilterConfig[LocalRateLimitFilterName] = protoconv.MessageToAny(localRateLimit)
}

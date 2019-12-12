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

package v1alpha3

import (
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	redis_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/redis_proxy/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
)

func TestBuildRedisFilter(t *testing.T) {
	redisFilter := buildRedisFilter("redis", "redis-cluster")
	if redisFilter.Name != xdsutil.RedisProxy {
		t.Errorf("redis filter name is %s not %s", redisFilter.Name, xdsutil.RedisProxy)
	}
	if config, ok := redisFilter.ConfigType.(*listener.Filter_TypedConfig); ok {
		redisProxy := redis_proxy.RedisProxy{}
		if err := ptypes.UnmarshalAny(config.TypedConfig, &redisProxy); err != nil {
			t.Errorf("unmarshal failed: %v", err)
		}
		if redisProxy.StatPrefix != "redis" {
			t.Errorf("redis proxy statPrefix is %s", redisProxy.StatPrefix)
		}
		if !redisProxy.LatencyInMicros {
			t.Errorf("redis proxy latency stat is not configured for microseconds")
		}
		if redisProxy.PrefixRoutes.CatchAllRoute.Cluster != "redis-cluster" {
			t.Errorf("redis proxy's PrefixRoutes.CatchAllCluster is %s", redisProxy.PrefixRoutes.CatchAllRoute.Cluster)
		}
	} else {
		t.Errorf("redis filter type is %T not listener.Filter_TypedConfig ", redisFilter.ConfigType)
	}
}

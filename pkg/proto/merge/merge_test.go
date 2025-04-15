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

package merge

import (
	"reflect"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"

	"istio.io/istio/pkg/test/util/assert"
)

// TestMergeDirect tests if we merge an object with a custom merge function, we use that function
// Previously, we only handled it for nested fields.
func TestMergeDirect(t *testing.T) {
	src := &durationpb.Duration{Nanos: 456}
	dst := &durationpb.Duration{Seconds: 789}

	Merge(dst, src)

	assert.Equal(t, dst, &durationpb.Duration{Nanos: 456})
	// src duration not changed after merge
	assert.Equal(t, src, &durationpb.Duration{Nanos: 456})
}

func TestMerge(t *testing.T) {
	src := &durationpb.Duration{Seconds: 123, Nanos: 456}
	dst := &durationpb.Duration{Seconds: 789, Nanos: 999}

	srcListener := &listener.Listener{ListenerFiltersTimeout: src}
	dstListener := &listener.Listener{ListenerFiltersTimeout: dst}

	Merge(dstListener, srcListener)

	assert.Equal(t, dstListener.ListenerFiltersTimeout, src)

	// dst duration not changed after merge
	assert.Equal(t, dst, &durationpb.Duration{Seconds: 789, Nanos: 999})
}

// Test that we can handle merge of Any types
func TestMergeAny(t *testing.T) {
	src := &cluster.Cluster{TypedExtensionProtocolOptions: map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			CommonHttpProtocolOptions: &core.HttpProtocolOptions{
				IdleTimeout:     durationpb.New(5 * time.Minute),
				MaxHeadersCount: wrapperspb.UInt32(5),
			},
		}),
	}}
	srcCpy := proto.Clone(src).(*cluster.Cluster)
	dst := &cluster.Cluster{TypedExtensionProtocolOptions: map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			CommonHttpProtocolOptions: &core.HttpProtocolOptions{
				IdleTimeout: durationpb.New(6 * time.Minute),
			},
			UpstreamProtocolOptions: &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
				UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
					HttpProtocolOptions: &core.Http1ProtocolOptions{AcceptHttp_10: true},
				},
			},
		}),
	}}

	Merge(dst, src)
	t.Log(Dump(t, dst))
	merged := &cluster.Cluster{TypedExtensionProtocolOptions: map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			CommonHttpProtocolOptions: &core.HttpProtocolOptions{
				IdleTimeout:     durationpb.New(5 * time.Minute),
				MaxHeadersCount: wrapperspb.UInt32(5),
			},
			UpstreamProtocolOptions: &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
				UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
					HttpProtocolOptions: &core.Http1ProtocolOptions{AcceptHttp_10: true},
				},
			},
		}),
	}}

	assert.Equal(t, dst, merged)
	//
	// Source should not change
	assert.Equal(t, src, srcCpy)
}

func Dump(t test.Failer, p proto.Message) string {
	v := reflect.ValueOf(p)
	if p == nil || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return "nil"
	}
	s, err := protomarshal.ToJSONWithIndent(p, "  ")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
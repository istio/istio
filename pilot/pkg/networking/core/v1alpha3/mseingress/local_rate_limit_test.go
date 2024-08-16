package mseingress

import (
	"testing"

	v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	local_ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/util/protoconv"
)

func TestGetLocalRateLimitFilter(t *testing.T) {
	rbac := &rbachttppb.RBAC{
		Rules: &rbacpb.RBAC{},
	}
	rbacFilter := &http_conn.HttpFilter{
		Name: "rbac",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(rbac),
		},
	}

	if GetLocalRateLimitFilter(rbacFilter) != nil {
		t.Fatal("should not have")
	}

	localRateLimit := &local_ratelimitv3.LocalRateLimit{
		StatPrefix: "test",
	}
	localRateLimitFilter := &http_conn.HttpFilter{
		Name: "local-rate-limit",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(localRateLimit),
		},
	}

	if !proto.Equal(GetLocalRateLimitFilter(localRateLimitFilter), localRateLimit) {
		t.Fatal("should equal")
	}

	rbacECDS := &http_conn.HttpFilter{
		Name: "ecds",
		ConfigType: &http_conn.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &v3.ExtensionConfigSource{
				TypeUrls: []string{RBACTypeUrl},
			},
		},
	}

	if GetLocalRateLimitFilter(rbacECDS) != nil {
		t.Fatal("should not have")
	}
}

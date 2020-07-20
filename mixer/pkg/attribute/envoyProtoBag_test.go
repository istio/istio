package attribute

import (
	"fmt"

	"github.com/golang/protobuf/ptypes/timestamp"

	//	"reflect"
	//"strconv"
	"testing"
	//"time"

	//mixerpb "istio.io/api/mixer/v1"
	//attr "istio.io/pkg/attribute"
	//"istio.io/pkg/log"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	authzGRPC "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
)

//var (
//	t9  = time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
//	t10 = time.Date(2001, 1, 1, 1, 1, 1, 10, time.UTC)
//	t42 = time.Date(2001, 1, 1, 1, 1, 1, 42, time.UTC)
//
//	d1 = 42 * time.Second
//	d2 = 34 * time.Second
//)

func TestBagEnvoy(t *testing.T) {
	attrs := &authzGRPC.CheckRequest{
		Attributes: &authzGRPC.AttributeContext{
			Source: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.1.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 52480,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-ratings",
			},
			Destination: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.2.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 9080,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-productpage",
			},
			Request: &authzGRPC.AttributeContext_Request{
				Time: &timestamp.Timestamp{
					Seconds: 1594395974,
					Nanos:   114093000,
				},
				Http: &authzGRPC.AttributeContext_HttpRequest{
					Id:     "4294822762638712056",
					Method: "POST",
					Headers: map[string]string{":authority": "productpage:9080", ":path": "/", ":method": "POST",
						":accept": "*/*", "content-length": "0"},
					Path:     "/",
					Host:     "productpage:9080",
					Protocol: "HTTP/1.1",
				},
			},
		},
	}
	fmt.Println(attrs)
}

//func init() {
//	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
//	o := log.DefaultOptions()
//	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
//	o.SetOutputLevel(scope.Name(), log.DebugLevel)
//	_ = log.Configure(o)
//}

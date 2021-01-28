package staticvm

import (
	"github.com/google/go-cmp/cmp"
	"istio.io/istio/pkg/test/framework/components/echo"
	"testing"
)

func TestVmcluster_CanDeploy(t *testing.T) {
	aSvc := "a"
	echoNS := fakeNamespace("echo")
	echoGenNS := fakeNamespace("echo-1234")
	ips := []string{"1.2.3.4"}
	vms := vmcluster{vms: []echo.Config{{
		Service: aSvc, Namespace: echoNS,
		StaticAddresses: ips,
	}}}

	for name, tc := range map[string]struct {
		given  echo.Config
		want   echo.Config
		wantOk bool
	}{
		"match": {
			given:  echo.Config{DeployAsVM: true, Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}},
			want:   echo.Config{DeployAsVM: true, Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}, StaticAddresses: ips},
			wantOk: true,
		},
		"non vm": {
			given: echo.Config{Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}},
		},
		"namespace mismatch": {
			given: echo.Config{DeployAsVM: true, Service: aSvc, Namespace: fakeNamespace("other"), Ports: []echo.Port{{Name: "grpc"}}},
		},
		"service mismatch": {
			given: echo.Config{DeployAsVM: true, Service: "b", Namespace: echoNS, Ports: []echo.Port{{Name: "grpc"}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := vms.CanDeploy(tc.given)
			if ok != tc.wantOk {
				t.Errorf("got %v but wanted %v", ok, tc.wantOk)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}

}

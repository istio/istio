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

package ambient

import (
	"fmt"
	"io"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	testKube "istio.io/istio/pkg/test/kube"
)

var _ io.Closer = &kubeComponent{}

type kubeComponent struct {
	id resource.ID

	sa       string
	ns       namespace.Instance
	inbound  istioKube.PortForwarder
	outbound istioKube.PortForwarder
}

func (k kubeComponent) Namespace() namespace.Instance {
	return k.ns
}

func (k kubeComponent) Inbound() string {
	return k.inbound.Address()
}

func (k kubeComponent) Outbound() string {
	return k.outbound.Address()
}

func (k kubeComponent) ID() resource.ID {
	return k.id
}

func (k kubeComponent) Close() error {
	k.inbound.Close()
	k.outbound.Close()
	return nil
}

// WaypointProxy describes a waypoint proxy deployment
type WaypointProxy interface {
	Namespace() namespace.Instance
	Inbound() string
	Outbound() string
}

// NewWaypointProxy creates a new WaypointProxy.
func NewWaypointProxy(ctx resource.Context, ns namespace.Instance, sa string) (WaypointProxy, error) {
	server := &kubeComponent{
		ns: ns,
		sa: sa,
	}
	server.id = ctx.TrackResource(server)

	// TODO: detect from UseWaypointProxy in echo.Config
	if err := ctx.ConfigIstio().Eval(ns.Name(), sa, `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: {{.}}-waypoint
  annotations:
    istio.io/service-account: {{.}}
spec:
  gatewayClassName: istio-mesh`).Apply(apply.NoCleanup); err != nil {
		return nil, err
	}
	cls := ctx.Clusters().Kube().Default()
	// Find the Prometheus pod and service, and start forwarding a local port.
	fetchFn := testKube.NewSinglePodFetch(cls, ns.Name(), fmt.Sprintf("istio.io/gateway-name=%s-waypoint", sa))
	pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]
	inbound, err := cls.NewPortForwarder(pod.Name, pod.Namespace, "", 0, 15008)
	if err != nil {
		return nil, err
	}

	if err := inbound.Start(); err != nil {
		return nil, err
	}
	outbound, err := cls.NewPortForwarder(pod.Name, pod.Namespace, "", 0, 15001)
	if err != nil {
		return nil, err
	}

	if err := outbound.Start(); err != nil {
		return nil, err
	}
	server.inbound = inbound
	server.outbound = outbound
	return server, nil
}

// NewWaypointProxyOrFail calls NewWaypointProxy and fails if an error occurs.
func NewWaypointProxyOrFail(t framework.TestContext, ns namespace.Instance, sa string) WaypointProxy {
	t.Helper()
	s, err := NewWaypointProxy(t, ns, sa)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

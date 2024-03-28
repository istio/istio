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
	"errors"
	"fmt"
	"io"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

var _ io.Closer = &kubeComponent{}

type kubeComponent struct {
	id resource.ID

	sa       string
	ns       namespace.Instance
	inbound  istioKube.PortForwarder
	outbound istioKube.PortForwarder
	pod      v1.Pod
}

func (k kubeComponent) Namespace() namespace.Instance {
	return k.ns
}

func (k kubeComponent) ServiceAccount() string {
	return k.sa
}

func (k kubeComponent) PodIP() string {
	return k.pod.Status.PodIP
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
	if k.inbound != nil {
		k.inbound.Close()
	}
	if k.outbound != nil {
		k.outbound.Close()
	}
	return nil
}

// WaypointProxy describes a waypoint proxy deployment
type WaypointProxy interface {
	Namespace() namespace.Instance
	ServiceAccount() string
	Inbound() string
	Outbound() string
	PodIP() string
}

// NewWaypointProxy creates a new WaypointProxy.
func NewWaypointProxy(ctx resource.Context, ns namespace.Instance, sa string) (WaypointProxy, error) {
	server := &kubeComponent{
		ns: ns,
		sa: sa,
	}
	server.id = ctx.TrackResource(server)
	if err := crd.DeployGatewayAPI(ctx); err != nil {
		return nil, err
	}

	// TODO support multicluster
	ik, err := istioctl.New(ctx, istioctl.Config{})
	if err != nil {
		return nil, err
	}
	// TODO: detect from UseWaypointProxy in echo.Config
	_, _, err = ik.Invoke([]string{
		"x",
		"waypoint",
		"apply",
		"--namespace",
		ns.Name(),
		"--service-account",
		sa,
	})
	if err != nil {
		return nil, err
	}

	cls := ctx.Clusters().Kube().Default()
	// Find the Waypoint pod and service, and start forwarding a local port.
	fetchFn := testKube.NewSinglePodFetch(cls, ns.Name(), fmt.Sprintf("%s=%s", constants.GatewayNameLabel, sa))
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
	server.pod = pod
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

// ------------------------------
// moved from bookinfo_test.go
// ------------------------------

func AddWaypointToService(t framework.TestContext, ns namespace.Instance, service, waypoint string) {
	if service != "" {
		cs := t.AllClusters().Configs()
		for _, c := range cs {
			oldSvc, err := c.Kube().CoreV1().Services(ns.Name()).Get(t.Context(), service, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error getting svc %s, err %v", service, err)
			}
			annotations := oldSvc.ObjectMeta.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string, 1)
			}
			annotations[constants.AmbientUseWaypoint] = waypoint
			oldSvc.ObjectMeta.SetAnnotations(annotations)
			_, err = c.Kube().CoreV1().Services(ns.Name()).Update(t.Context(), oldSvc, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("error updating svc %s, err %v", service, err)
			}
		}
	}
}

func DeleteWaypoint(t framework.TestContext, ns namespace.Instance, waypoint string) {
	istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
		"x",
		"waypoint",
		"delete",
		"--namespace",
		ns.Name(),
		"--service-account",
		waypoint,
	})
	waypointError := retry.UntilSuccess(func() error {
		fetch := testKube.NewPodFetch(t.AllClusters()[0], ns.Name(), constants.GatewayNameLabel+"="+waypoint)
		pods, err := testKube.CheckPodsAreReady(fetch)
		if err != nil && !errors.Is(err, testKube.ErrNoPodsFetched) {
			return fmt.Errorf("cannot fetch pod: %v", err)
		} else if len(pods) != 0 {
			return fmt.Errorf("waypoint pod is not deleted")
		}
		return nil
	}, retry.Timeout(time.Minute), retry.BackoffDelay(time.Millisecond*100))
	if waypointError != nil {
		t.Fatal(waypointError)
	}
}

func RemoveWaypointFromService(t framework.TestContext, ns namespace.Instance, service, waypoint string) {
	if service != "" {
		cs := t.AllClusters().Configs()
		for _, c := range cs {
			oldSvc, err := c.Kube().CoreV1().Services(ns.Name()).Get(t.Context(), service, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error getting svc %s, err %v", service, err)
			}
			annotations := oldSvc.ObjectMeta.GetAnnotations()
			if annotations != nil {
				delete(annotations, constants.AmbientUseWaypoint)
				oldSvc.ObjectMeta.SetAnnotations(annotations)
			}
			_, err = c.Kube().CoreV1().Services(ns.Name()).Update(t.Context(), oldSvc, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("error updating svc %s, err %v", service, err)
			}
		}
	}
}

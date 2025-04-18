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

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var _ io.Closer = &kubeComponent{}

type kubeComponent struct {
	id resource.ID

	ns       namespace.Instance
	inbound  istioKube.PortForwarder
	outbound istioKube.PortForwarder
	pod      v1.Pod
}

func (k kubeComponent) Namespace() namespace.Instance {
	return k.ns
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
	Inbound() string
	Outbound() string
	PodIP() string
}

// NewWaypointProxy creates a new WaypointProxy.
func NewWaypointProxy(ctx resource.Context, ns namespace.Instance, name string) (WaypointProxy, error) {
	server := &kubeComponent{
		ns: ns,
	}
	server.id = ctx.TrackResource(server)
	if err := crd.DeployGatewayAPI(ctx); err != nil {
		return nil, err
	}

	for _, cls := range ctx.AllClusters() {
		ik, err := istioctl.New(ctx, istioctl.Config{
			Cluster: cls,
		})
		if err != nil {
			return nil, err
		}
		// TODO: detect from UseWaypointProxy in echo.Config
		_, _, err = ik.Invoke([]string{
			"waypoint",
			"apply",
			"--namespace",
			ns.Name(),
			"--name",
			name,
			"--for",
			constants.AllTraffic,
		})
		if err != nil {
			return nil, err
		}
	}

	// TODO return a component per cluster for more targeted tests
	cls := ctx.Clusters().Default()
	// Find the Waypoint pod and service, and start forwarding a local port.
	fetchFn := testKube.NewSinglePodFetch(cls, ns.Name(), fmt.Sprintf("%s=%s", label.IoK8sNetworkingGatewayGatewayName.Name, name))
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
func NewWaypointProxyOrFail(t framework.TestContext, ns namespace.Instance, name string) WaypointProxy {
	t.Helper()
	s, err := NewWaypointProxy(t, ns, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func SetWaypointForService(t framework.TestContext, ns namespace.Instance, service, waypoint string) {
	if service == "" {
		return
	}

	cs := t.AllClusters()
	for _, c := range cs {
		oldSvc, err := c.Kube().CoreV1().Services(ns.Name()).Get(t.Context(), service, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("error getting svc %s, err %v", service, err)
		}
		oldLabels := oldSvc.ObjectMeta.GetLabels()
		if oldLabels == nil {
			oldLabels = make(map[string]string, 1)
		}
		newLabels := maps.Clone(oldLabels)
		if waypoint != "" {
			newLabels[label.IoIstioUseWaypoint.Name] = waypoint
		} else {
			delete(newLabels, label.IoIstioUseWaypoint.Name)
		}

		doLabel := func(labels map[string]string) error {
			// update needs the latest version
			svc, err := c.Kube().CoreV1().Services(ns.Name()).Get(t.Context(), service, metav1.GetOptions{})
			if err != nil {
				return err
			}
			svc.ObjectMeta.SetLabels(labels)
			_, err = c.Kube().CoreV1().Services(ns.Name()).Update(t.Context(), svc, metav1.UpdateOptions{})
			return err
		}

		if err = doLabel(newLabels); err != nil {
			t.Fatalf("error updating svc %s, err %v", service, err)
		}
		t.Cleanup(func() {
			if err := doLabel(oldLabels); err != nil {
				scopes.Framework.Errorf("failed resetting waypoint for %s/%s; this will likely break other tests", ns.Name(), service)
			}
		})

	}
}

func DeleteWaypoint(t framework.TestContext, ns namespace.Instance, waypoint string) {
	istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
		"waypoint",
		"delete",
		"--namespace",
		ns.Name(),
		waypoint,
	})
	waypointError := retry.UntilSuccess(func() error {
		fetch := testKube.NewPodFetch(t.AllClusters()[0], ns.Name(), label.IoK8sNetworkingGatewayGatewayName.Name+"="+waypoint)
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
			labels := oldSvc.ObjectMeta.GetLabels()
			if labels != nil {
				delete(labels, label.IoIstioUseWaypoint.Name)
				oldSvc.ObjectMeta.SetLabels(labels)
			}
			_, err = c.Kube().CoreV1().Services(ns.Name()).Update(t.Context(), oldSvc, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("error updating svc %s, err %v", service, err)
			}
		}
	}
}

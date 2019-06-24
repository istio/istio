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

package kube

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/kube"

	kubeCore "k8s.io/api/core/v1"
)

var (
	_ echo.Workload = &workload{}
)

type workload struct {
	*client.Instance

	addr      kubeCore.EndpointAddress
	pod       kubeCore.Pod
	forwarder kube.PortForwarder
	sidecar   *sidecar
}

func newWorkload(addr kubeCore.EndpointAddress, annotations echo.Annotations, grpcPort uint16, accessor *kube.Accessor) (*workload, error) {
	if addr.TargetRef == nil || addr.TargetRef.Kind != "Pod" {
		return nil, fmt.Errorf("invalid TargetRef for endpoint %s: %v", addr.IP, addr.TargetRef)
	}

	pod, err := accessor.GetPod(addr.TargetRef.Namespace, addr.TargetRef.Name)
	if err != nil {
		return nil, err
	}

	// Create a forwarder to the command port of the app.
	forwarder, err := accessor.NewPortForwarder(pod, 0, grpcPort)
	if err != nil {
		return nil, err
	}
	if err = forwarder.Start(); err != nil {
		return nil, err
	}

	// Create a gRPC client to this workload.
	c, err := client.New(forwarder.Address())
	if err != nil {
		_ = forwarder.Close()
		return nil, err
	}

	var s *sidecar
	if annotations.GetBool(echo.SidecarInject) {
		if s, err = newSidecar(pod, accessor); err != nil {
			return nil, err
		}
	}

	return &workload{
		addr:      addr,
		pod:       pod,
		forwarder: forwarder,
		Instance:  c,
		sidecar:   s,
	}, nil
}

func (w *workload) Close() (err error) {
	if w.Instance != nil {
		err = multierror.Append(err, w.Instance.Close()).ErrorOrNil()
	}
	if w.forwarder != nil {
		err = multierror.Append(err, w.forwarder.Close()).ErrorOrNil()
	}
	return
}

func (w *workload) Address() string {
	return w.addr.IP
}

func (w *workload) Sidecar() echo.Sidecar {
	return w.sidecar
}

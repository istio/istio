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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/resource"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/kube"

	kubeCore "k8s.io/api/core/v1"
)

const (
	appContainerName = "app"
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
	accessor  *kube.Accessor
	ctx       resource.Context
}

func newWorkload(addr kubeCore.EndpointAddress, annotations echo.Annotations, grpcPort uint16,
	accessor *kube.Accessor, ctx resource.Context) (*workload, error) {
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
		accessor:  accessor,
		ctx:       ctx,
	}, nil
}

func (w *workload) Close() (err error) {
	if w.Instance != nil {
		err = multierror.Append(err, w.Instance.Close()).ErrorOrNil()
	}
	if w.forwarder != nil {
		err = multierror.Append(err, w.forwarder.Close()).ErrorOrNil()
	}
	if w.ctx.Settings().FailOnDeprecation && w.sidecar != nil {
		err = multierror.Append(err, w.checkDeprecation()).ErrorOrNil()
	}
	return
}

func (w *workload) checkDeprecation() error {
	logs, err := w.sidecar.Logs()
	if err != nil {
		return fmt.Errorf("could not get sidecar logs to inspect for deprecation messages: %v", err)
	}

	info := fmt.Sprintf("pod: %s/%s", w.pod.Namespace, w.pod.Name)
	return errors.FindDeprecatedMessagesInEnvoyLog(logs, info)
}

func (w *workload) Address() string {
	return w.addr.IP
}

func (w *workload) Sidecar() echo.Sidecar {
	return w.sidecar
}

func (w *workload) Logs() (string, error) {
	return w.accessor.Logs(w.pod.Namespace, w.pod.Name, appContainerName, false)
}

func (w *workload) LogsOrFail(t test.Failer) string {
	t.Helper()
	logs, err := w.Logs()
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

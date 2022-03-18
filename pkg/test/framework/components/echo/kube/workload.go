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

package kube

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeCore "k8s.io/api/core/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	appContainerName = "app"
)

var _ echo.Workload = &workload{}

type workloadConfig struct {
	pod        kubeCore.Pod
	hasSidecar bool
	grpcPort   uint16
	cluster    cluster.Cluster
	tls        *common.TLSSettings
}

type workload struct {
	client *echoClient.Client

	workloadConfig
	forwarder  istioKube.PortForwarder
	sidecar    *sidecar
	ctx        resource.Context
	mutex      sync.Mutex
	connectErr error
}

func newWorkload(cfg workloadConfig, ctx resource.Context) (*workload, error) {
	w := &workload{
		workloadConfig: cfg,
		ctx:            ctx,
	}

	// If the pod is ready, connect.
	if err := w.Update(cfg.pod); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *workload) IsReady() bool {
	w.mutex.Lock()
	ready := w.isConnected()
	w.mutex.Unlock()
	return ready
}

func (w *workload) Client() (c *echoClient.Client, err error) {
	w.mutex.Lock()
	c = w.client
	if c == nil {
		err = fmt.Errorf("attempt to use disconnected client for echo %s/%s",
			w.pod.Namespace, w.pod.Name)
	}
	w.mutex.Unlock()
	return
}

func (w *workload) Update(pod kubeCore.Pod) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if isPodReady(pod) && !w.isConnected() {
		if err := w.connect(pod); err != nil {
			w.connectErr = err
			return err
		}
	} else if !isPodReady(pod) && w.isConnected() {
		w.pod = pod
		return w.disconnect()
	}

	// Update the pod.
	w.pod = pod
	return nil
}

func (w *workload) Close() (err error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.isConnected() {
		return w.disconnect()
	}
	return nil
}

func (w *workload) PodName() string {
	w.mutex.Lock()
	n := w.pod.Name
	w.mutex.Unlock()
	return n
}

func (w *workload) Address() string {
	w.mutex.Lock()
	ip := w.pod.Status.PodIP
	w.mutex.Unlock()
	return ip
}

func (w *workload) ForwardEcho(ctx context.Context, request *proto.ForwardEchoRequest) (echoClient.Responses, error) {
	w.mutex.Lock()
	c := w.client
	if c == nil {
		return nil, fmt.Errorf("failed forwarding echo for disconnected pod %s/%s",
			w.pod.Namespace, w.pod.Name)
	}
	w.mutex.Unlock()

	return c.ForwardEcho(ctx, request)
}

func (w *workload) Sidecar() echo.Sidecar {
	w.mutex.Lock()
	s := w.sidecar
	w.mutex.Unlock()
	return s
}

func (w *workload) Cluster() cluster.Cluster {
	return w.cluster
}

func (w *workload) Logs() (string, error) {
	return w.cluster.PodLogs(context.TODO(), w.pod.Name, w.pod.Namespace, appContainerName, false)
}

func (w *workload) LogsOrFail(t test.Failer) string {
	t.Helper()
	logs, err := w.Logs()
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

func isPodReady(pod kubeCore.Pod) bool {
	return istioKube.CheckPodReady(&pod) == nil
}

func (w *workload) isConnected() bool {
	return w.forwarder != nil
}

func (w *workload) connect(pod kubeCore.Pod) (err error) {
	defer func() {
		if err != nil {
			_ = w.disconnect()
		}
	}()

	// Create a forwarder to the command port of the app.
	if err = retry.UntilSuccess(func() error {
		w.forwarder, err = w.cluster.NewPortForwarder(pod.Name, pod.Namespace, "", 0, int(w.grpcPort))
		if err != nil {
			return fmt.Errorf("failed creating new port forwarder for pod %s/%s: %v",
				pod.Namespace, pod.Name, err)
		}
		if err = w.forwarder.Start(); err != nil {
			return fmt.Errorf("failed starting port forwarder for pod %s/%s: %v",
				pod.Namespace, pod.Name, err)
		}
		return nil
	}, retry.BackoffDelay(100*time.Millisecond), retry.Timeout(10*time.Second)); err != nil {
		return err
	}

	// Create a gRPC client to this workload.
	w.client, err = echoClient.New(w.forwarder.Address(), w.tls)
	if err != nil {
		return fmt.Errorf("failed connecting to grpc client to pod %s/%s : %v",
			pod.Namespace, pod.Name, err)
	}

	if w.hasSidecar {
		w.sidecar = newSidecar(pod, w.cluster)
	}

	return nil
}

func (w *workload) disconnect() (err error) {
	if w.client != nil {
		err = multierror.Append(err, w.client.Close()).ErrorOrNil()
		w.client = nil
	}
	if w.forwarder != nil {
		w.forwarder.Close()
		w.forwarder = nil
	}
	if w.ctx.Settings().FailOnDeprecation && w.sidecar != nil {
		err = multierror.Append(err, w.checkDeprecation()).ErrorOrNil()
		w.sidecar = nil
	}
	return err
}

func (w *workload) checkDeprecation() error {
	logs, err := w.sidecar.Logs()
	if err != nil {
		return fmt.Errorf("could not get sidecar logs to inspect for deprecation messages: %v", err)
	}

	info := fmt.Sprintf("pod: %s/%s", w.pod.Namespace, w.pod.Name)
	return errors.FindDeprecatedMessagesInEnvoyLog(logs, info)
}

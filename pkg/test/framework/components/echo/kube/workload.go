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
	corev1 "k8s.io/api/core/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	appContainerName = "app"
)

var _ echo.Workload = &workload{}

type workloadConfig struct {
	pod        corev1.Pod
	hasSidecar bool
	grpcPort   uint16
	cluster    cluster.Cluster
	tls        *common.TLSSettings
	stop       chan struct{}
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

	go watchPortForward(cfg, w)

	return w, nil
}

// watchPortForward wait watch the health of a port-forward connection. If a disconnect is detected, the workload is reconnected.
// TODO: this isn't structured very nicely. We have a port forwarder that can notify us when it fails (ErrChan) and we are competing with
// the pod informer which is sequenced via mutex. This could probably be cleaned up to be more event driven, but would require larger refactoring.
func watchPortForward(cfg workloadConfig, w *workload) {
	t := time.NewTicker(time.Millisecond * 500)
	handler := func() {
		w.mutex.Lock()
		defer w.mutex.Unlock()
		if w.forwarder == nil {
			// We only want to do reconnects here, if we never connected let the main flow handle it.
			return
		}
		// Only reconnect if the pod is ready
		if !isPodReady(w.pod) {
			return
		}
		con := !w.isConnected()
		if con {
			log.Warnf("pod: %s/%s port forward terminated", w.pod.Namespace, w.pod.Name)
			err := w.connect(w.pod)
			if err != nil {
				log.Warnf("pod: %s/%s port forward reconnect failed: %v", w.pod.Namespace, w.pod.Name, err)
			} else {
				log.Warnf("pod: %s/%s port forward reconnect success", w.pod.Namespace, w.pod.Name)
			}
		}
	}
	for {
		select {
		case <-cfg.stop:
			return
		case <-t.C:
			handler()
		}
	}
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
		err = fmt.Errorf("attempt to use disconnected client for echo pod %s/%s (in cluster %s)",
			w.pod.Namespace, w.pod.Name, w.cluster.Name())
	}
	w.mutex.Unlock()
	return c, err
}

func (w *workload) Update(pod corev1.Pod) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if isPodReady(pod) && !w.isConnected() {
		if err := w.connect(pod); err != nil {
			w.connectErr = err
			return err
		}
	} else if !isPodReady(pod) && w.isConnected() {
		scopes.Framework.Infof("echo pod %s/%s (in cluster %s) transitioned to NOT READY. Pod Status=%v",
			pod.Namespace, pod.Name, w.cluster.Name(), pod.Status.Phase)
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

func (w *workload) Addresses() []string {
	w.mutex.Lock()
	var addresses []string
	for _, podIP := range w.pod.Status.PodIPs {
		addresses = append(addresses, podIP.IP)
	}
	w.mutex.Unlock()
	return addresses
}

func (w *workload) ForwardEcho(ctx context.Context, request *proto.ForwardEchoRequest) (echoClient.Responses, error) {
	w.mutex.Lock()
	c := w.client
	if c == nil {
		w.mutex.Unlock()
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

func isPodReady(pod corev1.Pod) bool {
	return istioKube.CheckPodReady(&pod) == nil
}

func (w *workload) isConnected() bool {
	if w.forwarder == nil {
		return false
	}
	select {
	case <-w.forwarder.ErrChan():
		// If an error is available, we got disconnected
		return false
	default:
		// Otherwise we are connected
		return true
	}
}

func (w *workload) connect(pod corev1.Pod) (err error) {
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

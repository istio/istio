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
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/protocol"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	_ echo.Instance = &instance{}
	_ io.Closer     = &instance{}
)

type workloadHandler interface {
	WorkloadReady(w *workload)
	WorkloadNotReady(w *workload)
}

type workloadManager struct {
	workloads     []*workload
	mutex         sync.Mutex
	podController *podController
	cfg           echo.Config
	ctx           resource.Context
	grpcPort      uint16
	tls           *echoCommon.TLSSettings
	closing       bool
	stopCh        chan struct{}
	handler       workloadHandler
}

func newWorkloadManager(ctx resource.Context, cfg echo.Config, handler workloadHandler) (*workloadManager, error) {
	// Get the gRPC port and TLS settings.
	var grpcInstancePort int
	var tls *echoCommon.TLSSettings
	if cfg.IsProxylessGRPC() {
		grpcInstancePort = grpcMagicPort
	}
	if grpcInstancePort == 0 {
		if grpcPort, found := cfg.Ports.ForProtocol(protocol.GRPC); found {
			if grpcPort.TLS {
				tls = cfg.TLSSettings
			}
			grpcInstancePort = grpcPort.WorkloadPort
		}
	}
	if grpcInstancePort == 0 {
		return nil, errors.New("unable to find GRPC command port")
	}

	m := &workloadManager{
		cfg:      cfg,
		ctx:      ctx,
		handler:  handler,
		grpcPort: uint16(grpcInstancePort),
		tls:      tls,
		stopCh:   make(chan struct{}, 1),
	}
	m.podController = newPodController(cfg, podHandlers{
		added:   m.onPodAddOrUpdate,
		updated: m.onPodAddOrUpdate,
		deleted: m.onPodDeleted,
	})

	return m, nil
}

// WaitForReadyWorkloads waits until all known workloads are ready.
func (m *workloadManager) WaitForReadyWorkloads() (out echo.Workloads, err error) {
	err = retry.UntilSuccess(func() error {
		m.mutex.Lock()
		out, err = m.readyWorkloads()
		if err == nil && len(out) != len(m.workloads) {
			err = fmt.Errorf("failed waiting for workloads for echo %s/%s to be ready",
				m.cfg.Namespace.Name(),
				m.cfg.Service)
		}
		m.mutex.Unlock()
		return err
	}, retry.Timeout(m.cfg.ReadinessTimeout), startDelay)
	return out, err
}

func (m *workloadManager) readyWorkloads() (echo.Workloads, error) {
	out := make(echo.Workloads, 0, len(m.workloads))
	var connErrs error
	for _, w := range m.workloads {
		if w.IsReady() {
			out = append(out, w)
		} else if w.connectErr != nil {
			connErrs = multierror.Append(connErrs, w.connectErr)
		}
	}
	if len(out) == 0 {
		err := fmt.Errorf("no workloads ready for echo %s/%s", m.cfg.Namespace.Name(), m.cfg.Service)
		if connErrs != nil {
			err = fmt.Errorf("%v: failed connecting: %v", err, connErrs)
		}
		return nil, err
	}
	return out, nil
}

// ReadyWorkloads returns all ready workloads in ascending order by pod name.
func (m *workloadManager) ReadyWorkloads() (echo.Workloads, error) {
	m.mutex.Lock()
	out, err := m.readyWorkloads()
	m.mutex.Unlock()
	return out, err
}

func (m *workloadManager) Start() error {
	// Run the pod controller.
	go m.podController.Run(m.stopCh)

	// Wait for the cache to sync.
	if !m.podController.WaitForSync(m.stopCh) {
		return fmt.Errorf(
			"failed syncing cache for echo %s/%s: controller stopping",
			m.cfg.Namespace.Name(),
			m.cfg.Service)
	}

	// Wait until all pods are ready.
	_, err := m.WaitForReadyWorkloads()
	return err
}

func (m *workloadManager) onPodAddOrUpdate(pod *corev1.Pod) error {
	m.mutex.Lock()

	// After the method returns, notify the handler the ready state of the workload changed.
	var workloadReady *workload
	var workloadNotReady *workload
	defer func() {
		m.mutex.Unlock()

		if workloadReady != nil {
			m.handler.WorkloadReady(workloadReady)
		}
		if workloadNotReady != nil {
			m.handler.WorkloadNotReady(workloadNotReady)
		}
	}()

	// First, check to see if we already have a workload for the pod. If we do, just update it.
	for _, w := range m.workloads {
		if w.pod.Name == pod.Name {
			prevReady := w.IsReady()
			if err := w.Update(*pod); err != nil {
				return err
			}

			// Defer notifying the handler until after we release the mutex.
			if !prevReady && w.IsReady() {
				workloadReady = w
			} else if prevReady && !w.IsReady() {
				workloadNotReady = w
			}
			return nil
		}
	}

	// Add the pod to the end of the workload list.
	newWorkload, err := newWorkload(workloadConfig{
		pod:        *pod,
		hasSidecar: workloadHasSidecar(pod),
		cluster:    m.cfg.Cluster,
		grpcPort:   m.grpcPort,
		tls:        m.tls,
		stop:       m.stopCh,
	}, m.ctx)
	if err != nil {
		return err
	}
	m.workloads = append(m.workloads, newWorkload)

	if newWorkload.IsReady() {
		workloadReady = newWorkload
	}

	return nil
}

func (m *workloadManager) onPodDeleted(pod *corev1.Pod) (err error) {
	m.mutex.Lock()

	// After the method returns, notify the handler the ready state of the workload changed.
	var workloadNotReady *workload
	defer func() {
		m.mutex.Unlock()

		if workloadNotReady != nil {
			m.handler.WorkloadNotReady(workloadNotReady)
		}
	}()

	newWorkloads := make([]*workload, 0, len(m.workloads))
	for _, w := range m.workloads {
		if w.pod.Name == pod.Name {
			// Close the workload and remove it from the list. If an
			// error occurs, just continue.
			if w.IsReady() {
				workloadNotReady = w
			}
			err = w.Close()
		} else {
			// Just add all other workloads.
			newWorkloads = append(newWorkloads, w)
		}
	}

	m.workloads = newWorkloads
	return err
}

func (m *workloadManager) Close() (err error) {
	m.mutex.Lock()

	// Indicate we're closing.
	m.closing = true

	// Stop the controller and queue.
	close(m.stopCh)

	// Clear out the workloads array
	workloads := m.workloads
	m.workloads = nil

	m.mutex.Unlock()

	// Close the workloads.
	for _, w := range workloads {
		err = multierror.Append(err, w.Close()).ErrorOrNil()
	}
	return err
}

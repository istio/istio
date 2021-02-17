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
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	kubeCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/protocol"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
	kubeTest "istio.io/istio/pkg/test/kube"
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
	grpcPort := common.GetPortForProtocol(&cfg, protocol.GRPC)
	if grpcPort == nil {
		return nil, errors.New("unable fo find GRPC command port")
	}
	var tls *echoCommon.TLSSettings
	if grpcPort.TLS {
		tls = cfg.TLSSettings
	}

	m := &workloadManager{
		cfg:      cfg,
		ctx:      ctx,
		handler:  handler,
		grpcPort: uint16(grpcPort.InstancePort),
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

// ReadyWorkloads returns all ready workloads in ascending order by pod name.
func (m *workloadManager) ReadyWorkloads() ([]echo.Workload, error) {
	workloads := make([]echo.Workload, 0, len(m.workloads))

	m.mutex.Lock()
	for _, workload := range m.workloads {
		if workload.IsReady() {
			workloads = append(workloads, workload)
		}
	}
	var err error
	if len(workloads) == 0 {
		err = fmt.Errorf("no workloads ready for echo %s/%s", m.cfg.Namespace.Name(), m.cfg.Service)
	}
	m.mutex.Unlock()

	return workloads, err
}

func (m *workloadManager) Start() error {
	// Run the pod controller.
	go m.podController.Run(m.stopCh)

	// Wait for the cache to sync.
	if !m.podController.WaitForSync(m.stopCh) {
		return fmt.Errorf(
			"failed synching cache for echo %s/%s: controller stopping",
			m.cfg.Namespace.Name(),
			m.cfg.Service)
	}

	// Wait until all pods are ready.
	_, err := retry.Do(func() (result interface{}, completed bool, err error) {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		if m.closing {
			// Terminal error... exit the retry loop now.
			return nil, true, fmt.Errorf(
				"failed starting echo %s/%s: already closing",
				m.cfg.Namespace.Name(), m.cfg.Service)
		}

		// Gather all the pods.
		var pods []kubeCore.Pod
		for _, workload := range m.workloads {
			pods = append(pods, workload.pod)
		}
		if len(pods) == 0 {
			return nil, false, fmt.Errorf(
				"failed starting echo %s/%s: No ready pods found",
				m.cfg.Namespace.Name(), m.cfg.Service)
		}

		// Make sure that all pods are ready.
		fetchFn := func() ([]kubeCore.Pod, error) {
			return pods, nil
		}
		if _, err = kubeTest.CheckPodsAreReady(fetchFn); err != nil {
			return nil, false, err
		}

		// We've successfully started.
		return nil, true, nil
	}, retry.Timeout(m.cfg.ReadinessTimeout), startDelay)

	return err
}

func (m *workloadManager) onPodAddOrUpdate(pod *kubeCore.Pod) error {
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

	podWorkload := func() (*workload, error) {
		return newWorkload(workloadConfig{
			pod:        *pod,
			hasSidecar: workloadHasSidecar(m.cfg, pod.Name),
			cluster:    m.cfg.Cluster,
			grpcPort:   m.grpcPort,
			tls:        m.tls,
		}, m.ctx)
	}

	// Update the workloads list, in ascending order by pod name.
	added := false
	newWorkloads := make([]*workload, 0, len(m.workloads))
	for _, workload := range m.workloads {
		switch strings.Compare(workload.pod.Name, pod.Name) {
		case -1:
			// This workload comes before this pod alphabetically. Just add it
			// the the output list.
			newWorkloads = append(newWorkloads, workload)
		case 0:
			// We found the workload for this pod. Update it.
			prevReady := workload.IsReady()
			if err := workload.Update(*pod); err != nil {
				return err
			}
			if !prevReady && workload.IsReady() {
				workloadReady = workload
			} else if prevReady && !workload.IsReady() {
				workloadNotReady = workload
			}
			newWorkloads = append(newWorkloads, workload)
			added = true
		case 1:
			// The pod comes before the workload. Add it first.
			newWorkload, err := podWorkload()
			if err != nil {
				return err
			}
			newWorkloads = append(newWorkloads, newWorkload)

			if newWorkload.IsReady() {
				workloadReady = newWorkload
			}

			// Now add the previous workload.
			newWorkloads = append(newWorkloads, workload)
			added = true
		default:
			// Should never happen.
			panic("strings.Compare generated unexpected output for pod names")
		}
	}

	if !added {
		// The workload wasn't added. Add it now.
		newWorkload, err := podWorkload()
		if err != nil {
			return err
		}
		if newWorkload.IsReady() {
			workloadReady = newWorkload
		}
		newWorkloads = append(newWorkloads, newWorkload)
	}

	m.workloads = newWorkloads
	return nil
}

func (m *workloadManager) onPodDeleted(pod *kubeCore.Pod) (err error) {
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
	for _, workload := range m.workloads {
		if workload.pod.Name == pod.Name {
			// Close the workload and remove it from the list. If an
			// error occurs, just continue.
			if workload.IsReady() {
				workloadNotReady = workload
			}
			err = workload.Close()
		} else {
			// Just add all other workloads.
			newWorkloads = append(newWorkloads, workload)
		}
	}

	m.workloads = newWorkloads
	return err
}

func (m *workloadManager) Close() (err error) {
	// Stop the controller and queue.
	close(m.stopCh)

	// Clear out the workloads array
	m.mutex.Lock()
	m.closing = true
	workloads := m.workloads
	m.workloads = nil
	m.mutex.Unlock()

	// Close the workloads.
	for _, w := range workloads {
		err = multierror.Append(err, w.Close()).ErrorOrNil()
	}
	return
}

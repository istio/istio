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
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"

	kubeCore "k8s.io/api/core/v1"
)

var _ echo.Builder = &builder{}

type builder struct {
	ctx        resource.Context
	references []*echo.Instance
	configs    []echo.Config
	clusters   []resource.Cluster
}

func NewBuilder(ctx resource.Context) echo.Builder {
	return &builder{
		ctx:      ctx,
		clusters: ctx.Clusters()[:1],
	}
}

func (b *builder) WithClusters(clusters []resource.Cluster) echo.Builder {
	b.clusters = clusters
	return b
}

func (b *builder) With(inst *echo.Instance, cfg echo.Config) echo.Builder {
	clusters := b.clusters
	if cfg.Cluster != nil {
		clusters = []resource.Cluster{cfg.Cluster}
	}
	for i, c := range clusters {
		// create a cluster-specific copy of the config
		cfg := cfg
		cfg.Cluster = c
		if cfg.SetupFn != nil {
			cfg.SetupFn(&cfg)
		}
		b.configs = append(b.configs, cfg)

		// only track the reference for the first cluster
		ref := inst
		if i != 0 {
			ref = nil
		}
		b.references = append(b.references, ref)
	}
	return b
}

func (b *builder) Build() (echo.Result, error) {
	instances, err := b.newInstances()
	if err != nil {
		return nil, fmt.Errorf("build instance: %v", err)
	}

	if err := b.initializeInstances(instances); err != nil {
		return nil, fmt.Errorf("initialize instances: %v", err)
	}

	if err := b.waitUntilAllCallable(instances); err != nil {
		return nil, fmt.Errorf("wait until callable: %v", err)
	}

	// Success... update the caller's references.
	for i, inst := range instances {
		if b.references[i] == nil {
			continue
		}
		*b.references[i] = inst
	}
	return instances, nil
}

func (b *builder) BuildOrFail(t test.Failer) echo.Result {
	t.Helper()
	res, err := b.Build()
	if err != nil {
		t.Fatal(err)
		return nil
	}
	return res
}

func (b *builder) newInstances() ([]echo.Instance, error) {
	instances := make([]echo.Instance, 0, len(b.configs))
	for _, cfg := range b.configs {
		inst, err := newInstance(b.ctx, cfg)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

func (b *builder) initializeInstances(instances []echo.Instance) error {
	// Wait to receive the k8s Endpoints for each Echo Instance.
	wg := sync.WaitGroup{}
	instancePods := make([][]kubeCore.Pod, len(instances))
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for i, inst := range instances {
		wg.Add(1)

		instanceIndex := i
		inst := inst
		serviceName := inst.Config().Service
		serviceNamespace := inst.Config().Namespace.Name()
		timeout := inst.Config().ReadinessTimeout
		cluster := inst.(*instance).cluster

		// Run the waits in parallel.
		go func() {
			defer wg.Done()
			selector := "app"
			if inst.Config().DeployAsVM {
				selector = "istio.io/test-vm"
			}
			// Wait until all the pods are ready for this service
			fetch := kube.NewPodMustFetch(cluster, serviceNamespace, fmt.Sprintf("%s=%s", selector, serviceName))
			pods, err := kube.WaitUntilPodsAreReady(fetch, retry.Timeout(timeout))
			if err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, err)
				aggregateErrMux.Unlock()
				return
			}
			instancePods[instanceIndex] = pods
		}()
	}

	wg.Wait()

	if aggregateErr != nil {
		return aggregateErr
	}

	// Initialize the workloads for each instance.
	for i, inst := range instances {
		if err := inst.(*instance).initialize(instancePods[i]); err != nil {
			return fmt.Errorf("initialize %v: %v", inst.ID(), err)
		}
	}
	return nil
}

func (b *builder) waitUntilAllCallable(instances []echo.Instance) error {
	// Now wait for each endpoint to be callable from all others.
	wg := sync.WaitGroup{}
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for _, inst := range instances {
		wg.Add(1)

		source := inst
		go func() {
			defer wg.Done()

			if err := source.WaitUntilCallable(instances...); err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, err)
				aggregateErrMux.Unlock()
			}
		}()
	}
	wg.Wait()

	return aggregateErr
}

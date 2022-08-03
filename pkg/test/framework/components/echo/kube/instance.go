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
	"io"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/istiomultierror"
)

const (
	tcpHealthPort     = 3333
	httpReadinessPort = 8080
)

var (
	_ echo.Instance = &instance{}
	_ io.Closer     = &instance{}

	startDelay = retry.BackoffDelay(time.Millisecond * 100)
)

type instance struct {
	id             resource.ID
	cfg            echo.Config
	clusterIP      string
	clusterIPs     []string
	ctx            resource.Context
	cluster        cluster.Cluster
	workloadMgr    *workloadManager
	deployment     *deployment
	workloadFilter []echo.Workload
}

func newInstance(ctx resource.Context, originalCfg echo.Config) (out *instance, err error) {
	cfg := originalCfg.DeepCopy()

	c := &instance{
		cfg:     cfg,
		ctx:     ctx,
		cluster: cfg.Cluster,
	}

	// Deploy echo to the cluster
	c.deployment, err = newDeployment(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Create the manager for echo workloads for this instance.
	c.workloadMgr, err = newWorkloadManager(ctx, cfg, c.deployment)
	if err != nil {
		return nil, err
	}

	// Now that we have the successfully created the workload manager, track this resource so
	// that it will be closed when it goes out of scope.
	c.id = ctx.TrackResource(c)

	// Now retrieve the service information to find the ClusterIP
	s, err := c.cluster.Kube().CoreV1().Services(cfg.Namespace.Name()).Get(context.TODO(), cfg.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	c.clusterIP = s.Spec.ClusterIP
	c.clusterIPs = s.Spec.ClusterIPs
	switch c.clusterIP {
	case kubeCore.ClusterIPNone, "":
		if !cfg.Headless {
			return nil, fmt.Errorf("invalid ClusterIP %s for non-headless service %s/%s",
				c.clusterIP,
				c.cfg.Namespace.Name(),
				c.cfg.Service)
		}
		c.clusterIP = ""
	}

	// Start the workload manager.
	if err := c.workloadMgr.Start(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *instance) ID() resource.ID {
	return c.id
}

func (c *instance) Address() string {
	return c.clusterIP
}

func (c *instance) Addresses() []string {
	return c.clusterIPs
}

func (c *instance) Workloads() (echo.Workloads, error) {
	wls, err := c.workloadMgr.ReadyWorkloads()
	if err != nil {
		return nil, err
	}
	var final []echo.Workload
	for _, wl := range wls {
		filtered := false
		for _, filter := range c.workloadFilter {
			if wl.Address() != filter.Address() {
				filtered = true
				break
			}
		}
		if !filtered {
			final = append(final, wl)
		}
	}
	return final, nil
}

func (c *instance) WorkloadsOrFail(t test.Failer) echo.Workloads {
	t.Helper()
	out, err := c.Workloads()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (c *instance) MustWorkloads() echo.Workloads {
	out, err := c.Workloads()
	if err != nil {
		panic(err)
	}
	return out
}

func (c *instance) Clusters() cluster.Clusters {
	return cluster.Clusters{c.cluster}
}

func (c *instance) Instances() echo.Instances {
	return echo.Instances{c}
}

func (c *instance) Close() (err error) {
	return c.workloadMgr.Close()
}

func (c *instance) NamespacedName() echo.NamespacedName {
	return c.cfg.NamespacedName()
}

func (c *instance) PortForName(name string) echo.Port {
	return c.cfg.Ports.MustForName(name)
}

func (c *instance) ServiceName() string {
	return c.cfg.Service
}

func (c *instance) NamespaceName() string {
	return c.cfg.NamespaceName()
}

func (c *instance) ServiceAccountName() string {
	return c.cfg.ServiceAccountName()
}

func (c *instance) ClusterLocalFQDN() string {
	return c.cfg.ClusterLocalFQDN()
}

func (c *instance) ClusterSetLocalFQDN() string {
	return c.cfg.ClusterSetLocalFQDN()
}

func (c *instance) Config() echo.Config {
	return c.cfg
}

func (c *instance) WithWorkloads(wls ...echo.Workload) echo.Instance {
	n := *c
	c.workloadFilter = wls
	return &n
}

func (c *instance) Cluster() cluster.Cluster {
	return c.cfg.Cluster
}

func (c *instance) Call(opts echo.CallOptions) (echo.CallResult, error) {
	return c.aggregateResponses(opts)
}

func (c *instance) CallOrFail(t test.Failer, opts echo.CallOptions) echo.CallResult {
	t.Helper()
	r, err := c.Call(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (c *instance) GetWorkloadLabels(labels map[string]string) error {
	for _, wl := range c.workloadMgr.workloads {
		wl.mutex.Lock()
		pod := wl.pod
		wl.mutex.Unlock()
		if pod.Name != "" {
			pod.Labels = labels
			_, err := wl.Cluster().Kube().CoreV1().Pods(c.NamespaceName()).Update(context.TODO(), &pod, metav1.UpdateOptions{})
			return fmt.Errorf("update pod labels failed: %v", err)
		}
	}
	return nil
}

func (c *instance) UpdateWorkloadLabel(add map[string]string, remove []string) error {
	for _, wl := range c.workloadMgr.workloads {
		wl.mutex.Lock()
		pod := wl.pod
		wl.mutex.Unlock()
		if pod.Name != "" {
			return retry.UntilSuccess(func() (err error) {
				pod, err := wl.Cluster().Kube().CoreV1().Pods(c.NamespaceName()).Get(context.TODO(), pod.Name, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("get pod %s/%s failed: %v", pod.Namespace, pod.Name, err)
				}
				newLabels := make(map[string]string)
				for k, v := range pod.GetLabels() {
					newLabels[k] = v
				}
				for k, v := range add {
					newLabels[k] = v
				}
				for _, k := range remove {
					delete(newLabels, k)
				}
				pod.Labels = newLabels
				_, err = wl.Cluster().Kube().CoreV1().Pods(c.NamespaceName()).Update(context.TODO(), pod, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("update pod labels failed: %v", err)
				}
				return nil
			}, retry.Timeout(c.cfg.ReadinessTimeout), startDelay)
		}
	}
	return nil
}

func (c *instance) Restart() error {
	// Wait for all current workloads to become ready and preserve the original count.
	origWorkloads, err := c.workloadMgr.WaitForReadyWorkloads()
	if err != nil {
		return fmt.Errorf("restart failed to get initial workloads: %v", err)
	}

	// Restart the deployment.
	if err := c.deployment.Restart(); err != nil {
		return err
	}

	// Wait until all pods are ready and match the original count.
	return retry.UntilSuccess(func() (err error) {
		// Get the currently ready workloads.
		workloads, err := c.workloadMgr.WaitForReadyWorkloads()
		if err != nil {
			return fmt.Errorf("failed waiting for restarted pods for echo %s/%s: %v",
				c.cfg.Namespace.Name(), c.cfg.Service, err)
		}

		// Make sure the number of pods matches the original.
		if len(workloads) != len(origWorkloads) {
			return fmt.Errorf("failed restarting echo %s/%s: number of pods %d does not match original %d",
				c.cfg.Namespace.Name(), c.cfg.Service, len(workloads), len(origWorkloads))
		}

		return nil
	}, retry.Timeout(c.cfg.ReadinessTimeout), startDelay)
}

// aggregateResponses forwards an echo request from all workloads belonging to this echo instance and aggregates the results.
func (c *instance) aggregateResponses(opts echo.CallOptions) (echo.CallResult, error) {
	// TODO put this somewhere else, or require users explicitly set the protocol - quite hacky
	if c.Config().IsProxylessGRPC() && (opts.Scheme == scheme.GRPC || opts.Port.Name == "grpc" || opts.Port.Protocol == protocol.GRPC) {
		// for gRPC calls, use XDS resolver
		opts.Scheme = scheme.XDS
	}

	resps := make(echoClient.Responses, 0)
	workloads, err := c.Workloads()
	if err != nil {
		return echo.CallResult{}, err
	}
	aggErr := istiomultierror.New()
	for _, w := range workloads {
		clusterName := w.(*workload).cluster.Name()
		serviceName := fmt.Sprintf("%s (cluster=%s)", c.cfg.Service, clusterName)

		out, err := common.ForwardEcho(serviceName, c, opts, w.(*workload).Client)
		if err != nil {
			aggErr = multierror.Append(aggErr, err)
			continue
		}
		resps = append(resps, out.Responses...)
	}
	if aggErr.ErrorOrNil() != nil {
		return echo.CallResult{}, aggErr
	}

	return echo.CallResult{
		From:      c,
		Opts:      opts,
		Responses: resps,
	}, nil
}

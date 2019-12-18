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

package stackdriver

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"

	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"

	logging "google.golang.org/genproto/googleapis/logging/v2"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/grpc"
)

const (
	stackdriverNamespace = "istio-stackdriver"
	stackdriverPort      = 80
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id        resource.ID
	env       *kube.Environment
	ns        namespace.Instance
	forwarder testKube.PortForwarder
}

func newKube(ctx resource.Context) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		env: env,
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.CI.Info("=== BEGIN: Deploy Stackdriver ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("stackdriver deployment failed: %v", err) // nolint:golint
			scopes.CI.Infof("=== FAILED: Deploy Stackdriver ===")
			_ = c.Close()
		} else {
			scopes.CI.Info("=== SUCCEEDED: Deploy Stackdriver ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: stackdriverNamespace,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %s Namespace for Stackdriver install; err:%v", stackdriverNamespace, err)
	}

	// apply stackdriver YAML
	yamlContent, err := ioutil.ReadFile(environ.StackdriverInstallFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s, err: %v", environ.StackdriverInstallFilePath, err)
	}

	if err := env.ApplyContents(c.ns.Name(), string(yamlContent)); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %s, err: %v", environ.StackdriverInstallFilePath, err)
	}

	fetchFn := env.Accessor.NewSinglePodFetch(c.ns.Name(), "app=stackdriver")
	pods, err := env.Accessor.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	forwarder, err := env.Accessor.NewPortForwarder(pod, 0, stackdriverPort)
	if err != nil {
		return nil, err
	}

	if err := forwarder.Start(); err != nil {
		return nil, err
	}
	c.forwarder = forwarder
	scopes.Framework.Debugf("initialized stackdriver port forwarder: %v", forwarder.Address())

	return c, nil
}

func (c *kubeComponent) ListTimeSeries() ([]*monitoringpb.TimeSeries, error) {
	conn, err := grpc.Dial(c.forwarder.Address(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	mc := monitoringpb.NewMetricServiceClient(conn)
	if err != nil {
		return nil, err
	}
	r, err := mc.ListTimeSeries(context.Background(), &monitoringpb.ListTimeSeriesRequest{})
	if err != nil {
		return nil, err
	}
	var ret []*monitoringpb.TimeSeries
	for _, t := range r.TimeSeries {
		// Remove fields that do not need verification
		t.Points = nil
		delete(t.Resource.Labels, "project_id")
		delete(t.Resource.Labels, "cluster_name")
		delete(t.Resource.Labels, "location")
		ret = append(ret, t)
	}
	return ret, nil
}

func (c *kubeComponent) ListLogEntries() ([]*logging.LogEntry, error) {
	conn, err := grpc.Dial(c.forwarder.Address(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	lc := logging.NewLoggingServiceV2Client(conn)
	if err != nil {
		return nil, err
	}
	r, err := lc.ListLogEntries(context.Background(), &logging.ListLogEntriesRequest{})
	if err != nil {
		return nil, err
	}
	return r.Entries, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	scopes.CI.Infof("Deleting Stackdriver Install")
	_ = c.env.DeleteNamespace(stackdriverNamespace)
	_ = c.env.WaitForNamespaceDeletion(stackdriverNamespace)
	return nil
}

func (c *kubeComponent) GetStackdriverNamespace() string {
	return c.ns.Name()
}

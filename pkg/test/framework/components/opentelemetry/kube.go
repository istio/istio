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

package opentelemetry

import (
	"fmt"
	"io/ioutil"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
)

type otel struct {
	id      resource.ID
	cluster resource.Cluster
	close   func()
}

const (
	appName = "opentelemetry-collector"
)

func getYaml() (string, error) {
	b, err := ioutil.ReadFile(env.OtelCollectorInstallFilePath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func install(ctx resource.Context, ns string) error {
	y, err := getYaml()
	if err != nil {
		return err
	}
	return ctx.Config().ApplyYAML(ns, y)
}

func remove(ctx resource.Context, ns string) error {
	y, err := getYaml()
	if err != nil {
		return err
	}
	return ctx.Config().DeleteYAML(ns, y)
}

func newCollector(ctx resource.Context, c Config) (*otel, error) {
	o := &otel{
		cluster: ctx.Clusters().GetOrDefault(c.Cluster),
	}
	ctx.TrackResource(o)

	istioCfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	ns := istioCfg.TelemetryNamespace
	if err := install(ctx, ns); err != nil {
		return nil, err
	}

	o.close = func() {
		_ = remove(ctx, ns)
	}

	f := testKube.NewSinglePodFetch(o.cluster, ns, fmt.Sprintf("app=%s", appName))
	_, err = testKube.WaitUntilPodsAreReady(f)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (o *otel) ID() resource.ID {
	return o.id
}

// Close implements io.Closer.
func (o *otel) Close() error {
	if o.close != nil {
		o.close()
	}
	return nil
}

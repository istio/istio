// Copyright 2019 The prometheus-operator Authors
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

package e2e

import (
	"k8s.io/client-go/kubernetes"

	"github.com/Jeffail/gabs"
)

type prometheusClient struct {
	kubeClient kubernetes.Interface
}

func newPrometheusClient(kubeClient kubernetes.Interface) *prometheusClient {
	return &prometheusClient{kubeClient}
}

// Query makes a request against the Prometheus /api/v1/query endpoint.
func (c *prometheusClient) query(query string) (int, error) {
	req := c.kubeClient.CoreV1().RESTClient().Get().
		Namespace("monitoring").
		Resource("pods").
		SubResource("proxy").
		Name("prometheus-k8s-0:9090").
		Suffix("/api/v1/query").Param("query", query)

	b, err := req.DoRaw()
	if err != nil {
		return 0, err
	}

	res, err := gabs.ParseJSON(b)
	if err != nil {
		return 0, err
	}

	n, err := res.ArrayCountP("data.result")
	return n, err
}

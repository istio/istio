// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/util/assert"
)

func TestPrometheusPathAndPort(t *testing.T) {
	cases := []struct {
		pod  *v1.Pod
		path string
		port int
	}{
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-1",
					Annotations: map[string]string{
						"prometheus.io/path": "/metrics",
						"prometheus.io/port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-2",
					Annotations: map[string]string{
						"prometheus.io.path": "/metrics",
						"prometheus.io.port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "case-3",
					Annotations: map[string]string{
						"prometheus-io/path": "/metrics",
						"prometheus-io/port": "15020",
					},
				},
			},
			path: "/metrics",
			port: 15020,
		},
	}

	for _, tc := range cases {
		t.Run(tc.pod.Name, func(t *testing.T) {
			path, port, err := PrometheusPathAndPort(tc.pod)
			assert.NoError(t, err)
			assert.Equal(t, tc.path, path)
			assert.Equal(t, tc.port, port)
		})
	}
}

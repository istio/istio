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

package util

import (
	"context"
	"fmt"
	"strconv"

	"github.com/prometheus/prometheus/util/strutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
)

// CreateNamespace creates a namespace using the given k8s interface.
func CreateNamespace(cs kubernetes.Interface, namespace string, network string, dryRun bool) error {
	if dryRun {
		scope.Infof("Not applying Namespace %s because of dry run.", namespace)
		return nil
	}
	if namespace == "" {
		// Setup default namespace
		namespace = constants.IstioSystemNamespace
	}
	// check if the namespace already exists. If yes, do nothing. If no, create a new one.
	if _, err := cs.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{}); err != nil {
		if errors.IsNotFound(err) {
			ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   namespace,
				Labels: map[string]string{},
			}}
			if network != "" {
				ns.Labels[label.TopologyNetwork.Name] = network
			}
			_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create namespace %v: %v", namespace, err)
			}

			return nil
		}

		return fmt.Errorf("failed to check if namespace %v exists: %v", namespace, err)
	}

	return nil
}

func PrometheusPathAndPort(pod *v1.Pod) (string, int, error) {
	path := "/metrics"
	port := 9090
	for key, val := range pod.ObjectMeta.Annotations {
		switch strutil.SanitizeLabelName(key) {
		case "prometheus_io_port":
			p, err := strconv.Atoi(val)
			if err != nil {
				return "", 0, fmt.Errorf("failed to parse port from annotation: %v", err)
			}

			port = p
		case "prometheus_io_path":
			path = val
		}
	}

	return path, port, nil
}

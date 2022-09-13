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
	"errors"
	"fmt"
	"strings"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
	kubeCore "k8s.io/api/core/v1"

	// Import all XDS config types
	_ "istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	proxyContainerName = "istio-proxy"

	// DefaultTimeout the default timeout for the entire retry operation
	defaultConfigTimeout = time.Second * 30

	// DefaultDelay the default delay between successive retry attempts
	defaultConfigDelay = time.Millisecond * 100
)

var _ echo.Sidecar = &sidecar{}

type sidecar struct {
	podNamespace string
	podName      string
	cluster      cluster.Cluster
}

func newSidecar(pod kubeCore.Pod, cluster cluster.Cluster) *sidecar {
	sidecar := &sidecar{
		podNamespace: pod.Namespace,
		podName:      pod.Name,
		cluster:      cluster,
	}

	return sidecar
}

func (s *sidecar) Info() (*envoyAdmin.ServerInfo, error) {
	msg := &envoyAdmin.ServerInfo{}
	if err := s.adminRequest("server_info", msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *sidecar) InfoOrFail(t test.Failer) *envoyAdmin.ServerInfo {
	t.Helper()
	info, err := s.Info()
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func (s *sidecar) Config() (*envoyAdmin.ConfigDump, error) {
	msg := &envoyAdmin.ConfigDump{}
	if err := s.adminRequest("config_dump", msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *sidecar) ConfigOrFail(t test.Failer) *envoyAdmin.ConfigDump {
	t.Helper()
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (s *sidecar) WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error {
	options = append([]retry.Option{retry.BackoffDelay(defaultConfigDelay), retry.Timeout(defaultConfigTimeout)}, options...)

	var cfg *envoyAdmin.ConfigDump
	_, err := retry.UntilComplete(func() (result any, completed bool, err error) {
		cfg, err = s.Config()
		if err != nil {
			if strings.Contains(err.Error(), "could not resolve Any message type") {
				// Unable to parse an Any in the message, likely due to missing imports.
				// This is not a recoverable error.
				return nil, true, nil
			}
			if strings.Contains(err.Error(), `Any JSON doesn't have '@type'`) {
				// Unable to parse an Any in the message, likely due to an older version.
				// This is not a recoverable error.
				return nil, true, nil
			}
			return nil, false, err
		}

		accepted, err := accept(cfg)
		if err != nil {
			// Accept returned an error - retry.
			return nil, false, err
		}

		if accepted {
			// The configuration was accepted.
			return nil, true, nil
		}

		// The configuration was rejected, don't try again.
		return nil, true, errors.New("envoy config rejected")
	}, options...)
	if err != nil {
		configDumpStr := "nil"
		if cfg != nil {
			b, err := protomarshal.MarshalIndent(cfg, "  ")
			if err == nil {
				configDumpStr = string(b)
			}
		}

		return fmt.Errorf("failed waiting for Envoy configuration: %v. Last config_dump:\n%s", err, configDumpStr)
	}
	return nil
}

func (s *sidecar) WaitForConfigOrFail(t test.Failer, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) {
	t.Helper()
	if err := s.WaitForConfig(accept, options...); err != nil {
		t.Fatal(err)
	}
}

func (s *sidecar) Clusters() (*envoyAdmin.Clusters, error) {
	msg := &envoyAdmin.Clusters{}
	if err := s.adminRequest("clusters?format=json", msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *sidecar) ClustersOrFail(t test.Failer) *envoyAdmin.Clusters {
	t.Helper()
	clusters, err := s.Clusters()
	if err != nil {
		t.Fatal(err)
	}
	return clusters
}

func (s *sidecar) Listeners() (*envoyAdmin.Listeners, error) {
	msg := &envoyAdmin.Listeners{}
	if err := s.adminRequest("listeners?format=json", msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *sidecar) ListenersOrFail(t test.Failer) *envoyAdmin.Listeners {
	t.Helper()
	listeners, err := s.Listeners()
	if err != nil {
		t.Fatal(err)
	}
	return listeners
}

func (s *sidecar) Stats() (map[string]*dto.MetricFamily, error) {
	return s.proxyStats()
}

func (s *sidecar) StatsOrFail(t test.Failer) map[string]*dto.MetricFamily {
	t.Helper()
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func (s *sidecar) proxyStats() (map[string]*dto.MetricFamily, error) {
	// Exec onto the pod and make a curl request to the admin port, writing
	command := "pilot-agent request GET /stats/prometheus"
	stdout, stderr, err := s.cluster.PodExec(s.podName, s.podNamespace, proxyContainerName, command)
	if err != nil {
		return nil, fmt.Errorf("failed exec on pod %s/%s: %v. Command: %s. Output:\n%s",
			s.podNamespace, s.podName, err, command, stdout+stderr)
	}

	parser := expfmt.TextParser{}
	mfMap, err := parser.TextToMetricFamilies(strings.NewReader(stdout))
	if err != nil {
		return nil, fmt.Errorf("failed parsing prometheus stats: %v", err)
	}
	return mfMap, nil
}

func (s *sidecar) adminRequest(path string, out proto.Message) error {
	// Exec onto the pod and make a curl request to the admin port, writing
	command := fmt.Sprintf("pilot-agent request GET %s", path)
	stdout, stderr, err := s.cluster.PodExec(s.podName, s.podNamespace, proxyContainerName, command)
	if err != nil {
		return fmt.Errorf("failed exec on pod %s/%s: %v. Command: %s. Output:\n%s",
			s.podNamespace, s.podName, err, command, stdout+stderr)
	}

	if err := protomarshal.UnmarshalAllowUnknown([]byte(stdout), out); err != nil {
		return fmt.Errorf("failed parsing Envoy admin response from '/%s': %v\nResponse JSON: %s", path, err, stdout)
	}
	return nil
}

func (s *sidecar) Logs() (string, error) {
	return s.cluster.PodLogs(context.TODO(), s.podName, s.podNamespace, proxyContainerName, false)
}

func (s *sidecar) LogsOrFail(t test.Failer) string {
	t.Helper()
	logs, err := s.Logs()
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

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

package kube

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"

	v1 "k8s.io/api/core/v1"
)

const (
	proxyContainerName = "istio-proxy"
	proxyAdminPort     = 15000
)

var _ echo.Sidecar = &sidecar{}

type sidecar struct {
	nodeID       string
	podNamespace string
	podName      string
	accessor     *kube.Accessor
}

func newSidecar(pod v1.Pod, accessor *kube.Accessor) (*sidecar, error) {
	sidecar := &sidecar{
		podNamespace: pod.Namespace,
		podName:      pod.Name,
		accessor:     accessor,
	}

	// Extract the node ID from Envoy.
	if err := sidecar.WaitForConfig(func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		for _, c := range cfg.Configs {
			if c.TypeUrl == "type.googleapis.com/envoy.admin.v2alpha.BootstrapConfigDump" {
				cd := envoyAdmin.BootstrapConfigDump{}
				if err := types.UnmarshalAny(&c, &cd); err != nil {
					return false, err
				}

				sidecar.nodeID = cd.Bootstrap.Node.Id
				return true, nil
			}
		}
		return false, errors.New("envoy Bootstrap not found in config dump")
	}); err != nil {
		return nil, err
	}

	return sidecar, nil
}

func (s *sidecar) NodeID() string {
	return s.nodeID
}

func (s *sidecar) Info() (*envoyAdmin.ServerInfo, error) {
	msg := &envoyAdmin.ServerInfo{}
	if err := s.adminRequest("server_info", msg); err != nil {
		return nil, err
	}

	return msg, nil
}

func (s *sidecar) InfoOrFail(t testing.TB) *envoyAdmin.ServerInfo {
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

func (s *sidecar) ConfigOrFail(t testing.TB) *envoyAdmin.ConfigDump {
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (s *sidecar) WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error {
	return common.WaitForConfig(s.Config, accept, options...)
}

func (s *sidecar) WaitForConfigOrFail(t testing.TB, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) {
	if err := s.WaitForConfig(accept, options...); err != nil {
		t.Fatal(err)
	}
}

func (s *sidecar) adminRequest(path string, out proto.Message) error {
	// Exec onto the pod and make a curl request to the admin port, writing
	command := fmt.Sprintf("curl http://127.0.0.1:%d/%s", proxyAdminPort, path)
	response, err := s.accessor.Exec(s.podNamespace, s.podName, proxyContainerName, command)
	if err != nil {
		return fmt.Errorf("failed exec on pod %s/%s: %v. Command: %s. Output:\n%s",
			s.podNamespace, s.podName, err, command, response)
	}

	if err := jsonpb.Unmarshal(strings.NewReader(response), out); err != nil {
		return fmt.Errorf("failed parsing Envoy admin response from '/%s': %v\nResponse JSON: %s", path, err, response)
	}
	return nil
}

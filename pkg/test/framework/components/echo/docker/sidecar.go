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

package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/docker"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	proxyAdminPort = 15000
)

var _ echo.Sidecar = &sidecar{}

type sidecar struct {
	nodeID    string
	container *docker.Container
}

func newSidecar(container *docker.Container) (*sidecar, error) {
	sidecar := &sidecar{
		container: container,
	}

	// Extract the node ID from Envoy.
	if err := sidecar.WaitForConfig(func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		for _, c := range cfg.Configs {
			if c.TypeUrl == "type.googleapis.com/envoy.admin.v3.BootstrapConfigDump" {
				cd := envoyAdmin.BootstrapConfigDump{}
				if err := ptypes.UnmarshalAny(c, &cd); err != nil {
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
	return common.WaitForConfig(s.Config, accept, options...)
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

func (s *sidecar) adminRequest(path string, out proto.Message) error {
	// Exec onto the pod and make a curl request to the admin port, writing
	arg := fmt.Sprintf("http://%s:%d/%s", localhost, proxyAdminPort, path)
	result, err := s.container.Exec(context.Background(), "curl", arg)
	if err != nil {
		return fmt.Errorf("failed exec on container %s: %v. Command: curl %s. Output:\n%+v",
			s.container.Name, err, arg, result)
	}

	jspb := jsonpb.Unmarshaler{AllowUnknownFields: true}
	if err := jspb.Unmarshal(bytes.NewReader(result.StdOut), out); err != nil {
		return fmt.Errorf("failed parsing Envoy admin response from '/%s': %v\nStderr: %s\nStdout: %s",
			path, err, string(result.StdErr), string(result.StdOut))
	}
	return nil
}

func (s *sidecar) Logs() (string, error) {
	return s.container.Logs()
}

func (s *sidecar) LogsOrFail(t test.Failer) string {
	t.Helper()
	logs, err := s.Logs()
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

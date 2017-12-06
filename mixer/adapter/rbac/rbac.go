// Copyright 2017 Istio Authors.
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

package rbac

import (
  "context"

  "github.com/gogo/protobuf/types"
  rpc "github.com/googleapis/googleapis/google/rpc"
  "github.com/golang/glog"

  "istio.io/istio/mixer/pkg/adapter"
  "istio.io/istio/mixer/template/authorization"
  "istio.io/istio/mixer/pkg/runtime"
  "istio.io/istio/mixer/pkg/status"
)

type (
  builder struct {
  }
  handler struct {
    env adapter.Env
  }
)

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
  // Fetch all the ServiceRole/ServiceRoleBinding CRD objects, and
  return &handler{env: env}, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
  glog.Infof("Validate RBAC adapter")
  return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetAuthorizationTypes(types map[string]*authorization.Type) {
}

////////////////// Request-time Methods //////////////////////////
// authorization.Handler#HandleAuthorization
func (h *handler) HandleAuthorization(ctx context.Context, inst *authorization.Instance) (adapter.CheckResult, error) {
  glog.Infof("Handle request in RBAC adapter")
  s := rpc.Status{Code: int32(rpc.OK)}
  rbac := runtime.RbacInstance
  result, err := rbac.CheckPermission(inst)
  if !result || err != nil {
    s = status.WithPermissionDenied("RBAC: permission denied.")
  }
  return adapter.CheckResult{
    Status: s,
  }, nil
}

// adapter.Handler#Close
func (h *handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
  return adapter.Info{
    Name:        "rbac",
    Impl:        "istio.io/istio/mixer/adapter/rbac",
    Description: "Istio RBAC adapter",
    SupportedTemplates: []string{
      authorization.TemplateName,
    },
    NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
    DefaultConfig: &types.Empty{},
  }
}
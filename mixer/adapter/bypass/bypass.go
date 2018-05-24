//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/bypass/config/config.proto

package bypass

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all templates
//       known to Mixer. For now, it's manually curated.

import (
	"context"

	"github.com/gogo/protobuf/types"
	"go.uber.org/multierr"
	"google.golang.org/grpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/adapter/bypass/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "bypass",
		Impl:        "istio.io/istio/mixer/adapter/bypass",
		Description: "Calls gRPC backends via the inline adapter model (useful for testing)",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		DefaultConfig: &config.Params{},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
	}
}

type builder struct {
	params *config.Params
}

var _ metric.HandlerBuilder = &builder{}

func (b *builder) SetMetricTypes(map[string]*metric.Type) {}
func (b *builder) SetAdapterConfig(cfg adapter.Config)    { b.params = cfg.(*config.Params) }
func (b *builder) Validate() (ce *adapter.ConfigErrors)   { return }
func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {

	conn, err := grpc.Dial(b.params.BackendAddress, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	infraClient := v1beta1.NewInfrastructureBackendClient(conn)
	metricClient := metric.NewHandleMetricServiceClient(conn)

	req := v1beta1.CreateSessionRequest{
		AdapterConfig: b.params.Params,
	}

	resp, err := infraClient.CreateSession(context, &req)
	if err != nil {
		return nil, err
	}

	return &handler{
		config:       b.params.Params,
		session:      resp.SessionId,
		env:          env,
		conn:         conn,
		infraClient:  infraClient,
		metricClient: metricClient,
	}, nil
}

type handler struct {
	config       *types.Any
	session      string
	env          adapter.Env
	conn         *grpc.ClientConn
	infraClient  v1beta1.InfrastructureBackendClient
	metricClient metric.HandleMetricServiceClient
}

func (h *handler) HandleMetric(ctx context.Context, instances []*metric.Instance) error {
	request := metric.HandleMetricRequest{}

	request.DedupId = newDedupID()
	request.AdapterConfig = h.config

	for _, ins := range instances {
		value, err := convertValue(ins.Value)
		if err != nil {
			return err
		}
		dimensions, err := convertMapValue(ins.Dimensions)
		if err != nil {
			return err
		}
		instance := &metric.InstanceMsg{
			Name:       ins.Name,
			Value:      value,
			Dimensions: dimensions,
		}

		request.Instances = append(request.Instances, instance)
	}

	_, err := h.metricClient.HandleMetric(ctx, &request)

	return err
}

func (h *handler) Close() error {
	req := v1beta1.CloseSessionRequest{
		SessionId: h.session,
	}

	_, err := h.infraClient.CloseSession(context.TODO(), &req)

	err2 := h.conn.Close()

	return multierr.Append(err, err2)
}

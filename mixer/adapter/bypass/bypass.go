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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/bypass/config/config.proto -x "-n bypass -t checknothing -t reportnothing -t metric -t quota -d example"

package bypass

import (
	"context"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/adapter/bypass/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/template/reportnothing"
)

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("bypass")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

type builder struct {
	conn        *grpc.ClientConn
	infraClient v1beta1.InfrastructureBackendClient

	checknothingTypes  map[string]*checknothing.Type
	reportnothingTypes map[string]*reportnothing.Type
	metricTypes        map[string]*metric.Type
	quotaTypes         map[string]*quota.Type

	params *config.Params
}

var _ checknothing.HandlerBuilder = &builder{}
var _ reportnothing.HandlerBuilder = &builder{}
var _ metric.HandlerBuilder = &builder{}
var _ quota.HandlerBuilder = &builder{}

func (b *builder) SetCheckNothingTypes(checknothingTypes map[string]*checknothing.Type) {
	b.checknothingTypes = checknothingTypes
}

func (b *builder) SetReportNothingTypes(reportnothingTypes map[string]*reportnothing.Type) {
	b.reportnothingTypes = reportnothingTypes
}

func (b *builder) SetMetricTypes(metricTypes map[string]*metric.Type) {
	b.metricTypes = metricTypes
}

func (b *builder) SetQuotaTypes(quotaTypes map[string]*quota.Type) {
	b.quotaTypes = quotaTypes
}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.params = cfg.(*config.Params)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if err := b.ensureConn(); err != nil {
		ce = ce.Appendf("backend_address", "Unable to connect: %v", err)
		return
	}

	anyTypes, err := b.getInferredTypes()
	if err != nil {
		ce = ce.Appendf("infrerred_types", "Error marshaling to any: %v", err)
		return
	}

	req := v1beta1.ValidateRequest{
		AdapterConfig: b.params.Params,
		InferredTypes: anyTypes,
	}

	if b.params.SessionBased {
		resp, err := b.infraClient.Validate(context.TODO(), &req)
		if err != nil {
			ce = ce.Appendf("params", "error during validation: %v", err)
			return
		}

		if resp.Status.Code != int32(codes.OK) {
			ce = ce.Appendf("params", "validation error: %d/%s", resp.Status.Code, resp.Status.Message)
			return
		}
	}

	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	if err := b.ensureConn(); err != nil {
		return nil, err
	}

	anyTypes, err := b.getInferredTypes()
	if err != nil {
		return nil, err
	}
	req := v1beta1.CreateSessionRequest{
		AdapterConfig: b.params.Params,
		InferredTypes: anyTypes,
	}

	sessionID := ""
	if b.params.SessionBased {
		resp, err := b.infraClient.CreateSession(context, &req)
		if err != nil {
			return nil, err
		}
		sessionID = resp.SessionId
	}

	return &handler{
		params:              b.params,
		session:             sessionID,
		env:                 env,
		conn:                b.conn,
		infraClient:         b.infraClient,
		checknothingClient:  checknothing.NewHandleCheckNothingServiceClient(b.conn),
		reportnothingClient: reportnothing.NewHandleReportNothingServiceClient(b.conn),
		metricClient:        metric.NewHandleMetricServiceClient(b.conn),
		quotaClient:         quota.NewHandleQuotaServiceClient(b.conn),
	}, nil
}

func (b *builder) ensureConn() error {
	if b.conn == nil {
		bo := backoff.NewExponentialBackOff()
		bo.InitialInterval = time.Millisecond * 10
		bo.MaxElapsedTime = time.Minute * 5

		err := backoff.Retry(func() error {
			var e error
			b.conn, e = grpc.Dial(b.params.BackendAddress, grpc.WithInsecure())
			return e

		}, bo)

		if err != nil {
			return err
		}

		if b.params.SessionBased {
			b.infraClient = v1beta1.NewInfrastructureBackendClient(b.conn)
		}
	}

	return nil
}

func (b *builder) getInferredTypes() (result map[string]*types.Any, err error) {
	result = make(map[string]*types.Any)
	setAny := func(k string, v proto.Message) {
		by, e := proto.Marshal(v)
		if err != nil {
			err = e
			return
		}
		result[k] = &types.Any{
			Value:   by,
			TypeUrl: proto.MessageName(v),
		}
	}

	for k, v := range b.checknothingTypes {
		setAny(k, v)
	}
	for k, v := range b.reportnothingTypes {
		setAny(k, v)
	}
	for k, v := range b.metricTypes {
		setAny(k, v)
	}
	for k, v := range b.quotaTypes {
		setAny(k, v)
	}

	return
}

type handler struct {
	params              *config.Params
	session             string
	env                 adapter.Env
	conn                *grpc.ClientConn
	infraClient         v1beta1.InfrastructureBackendClient
	checknothingClient  checknothing.HandleCheckNothingServiceClient
	reportnothingClient reportnothing.HandleReportNothingServiceClient
	metricClient        metric.HandleMetricServiceClient
	quotaClient         quota.HandleQuotaServiceClient
}

var _ checknothing.Handler = &handler{}
var _ reportnothing.Handler = &handler{}
var _ metric.Handler = &handler{}
var _ quota.Handler = &handler{}

func (h *handler) HandleCheckNothing(ctx context.Context, instance *checknothing.Instance) (adapter.CheckResult, error) {
	request := checknothing.HandleCheckNothingRequest{}

	request.DedupId = newDedupID()
	if !h.params.SessionBased {
		request.AdapterConfig = h.params.Params
	}

	request.Instance = &checknothing.InstanceMsg{
		Name: instance.Name,
	}

	result := adapter.CheckResult{}
	response, err := h.checknothingClient.HandleCheckNothing(ctx, &request)
	if err == nil {
		result.Status = response.Status
		result.ValidDuration = response.ValidDuration
		result.ValidUseCount = response.ValidUseCount
	}

	return result, err
}

func (h *handler) HandleReportNothing(ctx context.Context, instances []*reportnothing.Instance) error {
	request := reportnothing.HandleReportNothingRequest{}

	request.DedupId = newDedupID()
	if !h.params.SessionBased {
		request.AdapterConfig = h.params.Params
	}

	for _, ins := range instances {
		instance := &reportnothing.InstanceMsg{
			Name: ins.Name,
		}

		request.Instances = append(request.Instances, instance)
	}

	_, err := h.reportnothingClient.HandleReportNothing(ctx, &request)

	return err
}

func (h *handler) HandleMetric(ctx context.Context, instances []*metric.Instance) error {
	request := metric.HandleMetricRequest{}

	request.DedupId = newDedupID()
	if !h.params.SessionBased {
		request.AdapterConfig = h.params.Params
	}

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

func (h *handler) HandleQuota(ctx context.Context, instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	request := quota.HandleQuotaRequest{}

	request.DedupId = args.DeduplicationID
	if !h.params.SessionBased {
		request.AdapterConfig = h.params.Params
	}

	result := adapter.QuotaResult{}

	dimensions, err := convertMapValue(instance.Dimensions)
	if err != nil {
		return result, err
	}

	request.Instance = &quota.InstanceMsg{
		Name:       instance.Name,
		Dimensions: dimensions,
	}

	request.QuotaRequest = &v1beta1.QuotaRequest{
		Quotas: make(map[string]v1beta1.QuotaRequest_QuotaParams),
	}
	request.QuotaRequest.Quotas[instance.Name] = v1beta1.QuotaRequest_QuotaParams{
		Amount:     args.QuotaAmount,
		BestEffort: args.BestEffort,
	}

	response, err := h.quotaClient.HandleQuota(ctx, &request)
	if err == nil {
		result.ValidDuration = response.Quotas[instance.Name].ValidDuration
		result.Amount = response.Quotas[instance.Name].GrantedAmount
	}

	return result, err
}

func (h *handler) Close() (err error) {
	req := v1beta1.CloseSessionRequest{
		SessionId: h.session,
	}

	if h.infraClient != nil {
		_, err = h.infraClient.CloseSession(context.TODO(), &req)
	}

	if h.conn != nil {
		err = multierror.Append(err, h.conn.Close()).ErrorOrNil()
	}

	return
}

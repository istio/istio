// Copyright 2017 Istio Authors
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

package servicecontrol

// Sample quota config:
// '''
// apiVersion: "config.istio.io/v1alpha2"
// kind: quota
// metadata:
//   name: qutoa-name
//   namespace: istio-system
// spec:
//   dimensions:
//     api_operation : api.operation
//     api_key : api.key
// '''

import (
	"context"
	"fmt"
	"time"

	rpc "github.com/gogo/googleapis/google/rpc"
	"github.com/pborman/uuid"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/quota"
)

const (
	// Dimension name used in Quota instance config
	apiKeyDimension       = "api_key"
	apiOperationDimension = "api_operation"

	// Google Service Control quota mode
	quotaModeNormal     = "NORMAL"
	quotaModeBestEffort = "BEST_EFFORT"

	// Metric and label returned by Google Service Control containing quota allocation result.
	quotaAllocationResultMetric = "serviceruntime.googleapis.com/api/consumer/quota_used_count"
	quotaNameLabel              = "/quota_name"
)

// Implementation of quotaProcessor interface
type quotaImpl struct {
	env            adapter.Env
	serviceSetting *config.GcpServiceSetting
	// An index from Istio quota name to quota config in serviceSetting
	quotaIndex map[string]*config.Quota
	client     serviceControlClient
}

func getQuotaMode(args adapter.QuotaArgs) string {
	if args.BestEffort {
		return quotaModeBestEffort
	}
	return quotaModeNormal
}

func createQuotaResult(status rpc.Status, expiration time.Duration, amount int64) adapter.QuotaResult {
	return adapter.QuotaResult{
		Status:        status,
		ValidDuration: expiration,
		Amount:        amount,
	}
}

func quotaResultFromQuotaError(quotaErr *sc.QuotaError, quotaCfg *config.Quota) adapter.QuotaResult {
	code := serviceControlErrorToRPCCode(quotaErr.Code)
	return createQuotaResult(status.WithMessage(code, quotaErr.Description),
		toDuration(quotaCfg.Expiration), 0)
}

func makeQuotaResult(response *sc.AllocateQuotaResponse, quotaCfg *config.Quota,
	args *adapter.QuotaArgs) adapter.QuotaResult {
	succeeded := response.AllocateErrors == nil || len(response.AllocateErrors) == 0
	if !succeeded {
		return quotaResultFromQuotaError(response.AllocateErrors[0], quotaCfg)
	}

	// If quota metric is not found in response, just return the request quota amount
	if response.QuotaMetrics == nil || len(response.QuotaMetrics) == 0 {
		return createQuotaResult(status.OK, toDuration(quotaCfg.Expiration), args.QuotaAmount)
	}

	return createQuotaResult(status.OK, toDuration(quotaCfg.Expiration),
		getAllocatedQuotaAmount(args, response.QuotaMetrics, quotaCfg))
}

func buildAllocateRequest(instance *quota.Instance,
	args adapter.QuotaArgs, quotaCfg *config.Quota) (*sc.AllocateQuotaRequest, error) {
	rawAPIKey, ok := dimensionToString(instance.Dimensions, apiKeyDimension)
	if !ok {
		return nil, fmt.Errorf("dimension %v not found in instance %v", apiKeyDimension, *instance)
	}

	apiOperation, ok := dimensionToString(instance.Dimensions, apiOperationDimension)
	if !ok {
		return nil, fmt.Errorf("dimension %v not found in instance %v", apiOperationDimension, *instance)
	}

	// If GoogleQuotaMetric name is not set in config, assume Istio quota name is Google's quota metric name.
	metricName := quotaCfg.GoogleQuotaMetricName
	if metricName == "" {
		metricName = quotaCfg.Name
	}
	apiKey := generateConsumerIDFromAPIKey(rawAPIKey)
	op := &sc.QuotaOperation{
		ConsumerId:  apiKey,
		MethodName:  apiOperation,
		OperationId: uuid.New(),
		QuotaMetrics: []*sc.MetricValueSet{
			{
				MetricName: metricName,
				MetricValues: []*sc.MetricValue{
					{
						Int64Value: getInt64Address(args.QuotaAmount),
					},
				},
			},
		},
		QuotaMode: getQuotaMode(args),
	}

	return &sc.AllocateQuotaRequest{
		AllocateOperation: op,
	}, nil
}

func getAllocatedQuotaAmount(args *adapter.QuotaArgs, quotaMetrics []*sc.MetricValueSet,
	quotaCfg *config.Quota) int64 {
	// Initialize allocated amount to requested amount
	var amount = args.QuotaAmount
	for _, metric := range quotaMetrics {
		if metric.MetricName == quotaAllocationResultMetric &&
			metric.MetricValues != nil {
			for _, value := range metric.MetricValues {
				if value.Labels != nil && value.Labels[quotaNameLabel] == quotaCfg.GoogleQuotaMetricName {
					amount = *value.Int64Value
					break
				}
			}
		}
	}
	return amount
}

// ProcessQuota calls Google ServiceControl client to allocate requested quota.
// TODO(manlinl): Support handling multiple QuotaArgs once Mixer supports it. And implement retry on retriable error.
func (p *quotaImpl) ProcessQuota(ctx context.Context,
	instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	quotaCfg, found := p.quotaIndex[instance.Name]
	if !found {
		errMsg := "unknown quota name: " + instance.Name
		return createQuotaResult(
				status.WithInvalidArgument(errMsg), time.Minute, 0),
			fmt.Errorf(errMsg)
	}

	quotaDuration := toDuration(quotaCfg.Expiration)
	request, err := buildAllocateRequest(instance, args, quotaCfg)
	if err != nil {
		return createQuotaResult(status.WithInvalidArgument(err.Error()),
			quotaDuration, 0), err
	}

	if p.env.Logger().DebugEnabled() {
		if requestDetail, err := toFormattedJSON(request); err == nil {
			p.env.Logger().Debugf("Quota request :%v", requestDetail)
		}
	}

	response, err := p.client.AllocateQuota(
		p.serviceSetting.GoogleServiceName, request)
	if err != nil {
		err = p.env.Logger().Errorf("allocate quota failed: %v", err)
		return createQuotaResult(status.WithError(err),
			quotaDuration, 0), err
	}

	if p.env.Logger().DebugEnabled() {
		if responseDetail, err := toFormattedJSON(response); err == nil {
			p.env.Logger().Debugf("response :%v", responseDetail)
		}
	}

	return makeQuotaResult(response, quotaCfg, &args), nil
}

func newQuotaProcessor(meshServiceName string, ctx *handlerContext) (*quotaImpl, error) {
	svcCfg, found := ctx.serviceConfigIndex[meshServiceName]
	if !found {
		return nil, fmt.Errorf("mesh service not found: %s", meshServiceName)
	}

	processor := &quotaImpl{
		ctx.env,
		svcCfg,
		make(map[string]*config.Quota),
		ctx.client,
	}

	for _, quotaCfg := range svcCfg.Quotas {
		processor.quotaIndex[quotaCfg.Name] = quotaCfg
	}
	return processor, nil
}

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

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	rpc "github.com/gogo/googleapis/google/rpc"
	"github.com/pborman/uuid"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/pkg/cache"
)

// checkImpl implements checkProcessor interface, handles doCheck call to Google ServiceControl backend.
type checkImpl struct {
	env                   adapter.Env
	checkResultExpiration time.Duration
	// A LRU cache, keyed by checkCacheKey struct and value is of type *sc.CheckResponse
	responseCache cache.ExpiringCache
	runtimeConfig *config.RuntimeConfig
	serviceConfig *config.GcpServiceSetting
	client        serviceControlClient
}

// Cache key used with checkImpl.responseCache. The cache is at handler level, so it stores check responses for multiple
// GCP services.
type checkCacheKey struct {
	googleServiceName string
	consumerID        string
	operation         string
}

// ProcessCheck processes check call and converts CheckResponse to adapter.CheckResult.
func (c *checkImpl) ProcessCheck(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	if instance.ApiKey == "" || instance.ApiOperation == "" {
		return c.checkResult(
			status.WithInvalidArgument(
				fmt.Sprintf(
					"instance:%s, api key and api operation must not be empty", instance.Name))), nil
	}
	consumerID := generateConsumerIDFromAPIKey(instance.ApiKey)
	response, err := c.doCheck(consumerID, instance.ApiOperation, instance.Timestamp)
	if err != nil {
		return c.checkResult(status.WithPermissionDenied(err.Error())), nil
	}

	return c.responseToCheckResult(response)
}

// ResolveConsumerProjectID resolves consumer project ID from consumer ID and operation name.
func (c *checkImpl) ResolveConsumerProjectID(consumerID, opName string) (string, error) {
	response, err := c.doCheck(consumerID, opName, time.Now())
	if err != nil {
		return "", nil
	}

	if response == nil || response.CheckInfo == nil || response.CheckInfo.ConsumerInfo == nil {
		return "", errors.New("consumer info missing from CheckResponse")
	}

	return fmt.Sprintf("project_number:%d",
		response.CheckInfo.ConsumerInfo.ProjectNumber), nil
}

// doCheck calls Check on Google ServiceControl client.
func (c *checkImpl) doCheck(consumerID, operationName string, timestamp time.Time) (*sc.CheckResponse, error) {
	cacheKey := checkCacheKey{
		googleServiceName: c.serviceConfig.GoogleServiceName,
		consumerID:        consumerID,
		operation:         operationName,
	}

	cachedResponse, found := c.responseCache.Get(cacheKey)
	if found {
		return cachedResponse.(*sc.CheckResponse), nil
	}

	request := &sc.CheckRequest{
		Operation: &sc.Operation{
			OperationId:   uuid.New(),
			OperationName: operationName,
			StartTime:     timestamp.Format(time.RFC3339),
			ConsumerId:    consumerID,
		},
	}

	if c.env.Logger().DebugEnabled() {
		if requestDetail, err := toFormattedJSON(request); err == nil {
			c.env.Logger().Debugf("request: %v", requestDetail)
		}
	}

	response, err := c.client.Check(c.serviceConfig.GoogleServiceName, request)
	if err != nil {
		return nil, err
	}

	if c.env.Logger().DebugEnabled() {
		if responseDetail, err := toFormattedJSON(response); err == nil {
			c.env.Logger().Debugf("response: %v", responseDetail)
		}
	}

	c.responseCache.Set(cacheKey, response)
	return response, nil
}

// responseToCheckResult converts ServiceControl CheckResponse to Mixer CheckerResult
func (c *checkImpl) responseToCheckResult(response *sc.CheckResponse) (adapter.CheckResult, error) {
	result := c.checkResult(status.OK)

	if response.ServerResponse.HTTPStatusCode != 200 {
		code := toRPCCode(response.ServerResponse.HTTPStatusCode)
		result.Status = status.New(code)
	}

	if len(response.CheckErrors) > 0 {
		checkError := response.CheckErrors[0]
		result.Status = status.WithMessage(serviceControlErrorToRPCCode(checkError.Code),
			fmt.Sprintf("%s: %s", checkError.Code, checkError.Detail))
	}

	return result, nil
}

func (c *checkImpl) checkResult(status rpc.Status) adapter.CheckResult {
	return adapter.CheckResult{
		Status:        status,
		ValidDuration: c.checkResultExpiration,
		ValidUseCount: math.MaxInt32,
	}
}

func newCheckProcessor(meshServiceName string, ctx *handlerContext) (*checkImpl, error) {
	serviceConfig, found := ctx.serviceConfigIndex[meshServiceName]
	if !found {
		return nil, fmt.Errorf("unknown mesh service %v", meshServiceName)
	}

	return &checkImpl{
		ctx.env,
		toDuration(ctx.config.RuntimeConfig.CheckResultExpiration),
		ctx.checkResponseCache,
		ctx.config.RuntimeConfig,
		serviceConfig,
		ctx.client,
	}, nil
}

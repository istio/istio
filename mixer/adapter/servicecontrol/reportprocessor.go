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
	"fmt"
	"time"

	"github.com/pborman/uuid"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	"istio.io/istio/mixer/pkg/adapter"
)

type (
	reportImpl struct {
		env           adapter.Env
		serviceConfig *config.GcpServiceSetting
		client        serviceControlClient
		resolver      consumerProjectIDResolver
	}
)

// ProcessReport processes servicecontrolreport data point and send metrics and logs via Google ServiceControl API.
func (r *reportImpl) ProcessReport(ctx context.Context, instances []*servicecontrolreport.Instance) error {
	logger := r.env.Logger()
	for _, inst := range instances {
		builder := newReportBuilder(inst, supportedMetrics, r.resolver)
		op := initializeOperation(inst)
		builder.build(op)
		if len(op.MetricValueSets) == 0 && len(op.LogEntries) == 0 {
			logger.Warningf("no metric or log entry is generated, dimensions: %v", inst)
			continue
		}
		r.scheduleReport(op)
	}
	return nil
}

func (r *reportImpl) Close() error {
	return nil
}

// TODO(manlinl): Retry if report is failed to send.
func (r *reportImpl) scheduleReport(op *sc.Operation) {
	r.env.ScheduleWork(func() {
		logger := r.env.Logger()
		request := &sc.ReportRequest{
			Operations: []*sc.Operation{op},
		}

		response, err := r.client.Report(r.serviceConfig.GoogleServiceName, request)
		if err != nil || response.ReportErrors != nil {
			logger.Errorf("fail to send report: %v", err)
		}

		if logger.VerbosityLevel(logDebug) {
			if requestDetail, err := toFormattedJSON(request); err == nil {
				logger.Infof("request: %v", requestDetail)
			}
			if response != nil {
				if responseDetail, err := toFormattedJSON(response); err == nil {
					logger.Infof("response:%v", responseDetail)
				}
			}
		}
	})
}

func initializeOperation(inst *servicecontrolreport.Instance) *sc.Operation {
	op := &sc.Operation{}
	op.ConsumerId, _ = generateConsumerID(inst)
	op.OperationId = uuid.New()
	op.OperationName = inst.ApiOperation

	op.StartTime = inst.RequestTime.UTC().Format(time.RFC3339Nano)
	op.EndTime = inst.ResponseTime.UTC().Format(time.RFC3339Nano)
	return op
}

func newReportProcessor(meshServiceName string, ctx *handlerContext,
	resolver consumerProjectIDResolver) (*reportImpl, error) {
	serviceConfig, found := ctx.serviceConfigIndex[meshServiceName]
	if !found {
		return nil, fmt.Errorf("unknown mesh service:%v", meshServiceName)
	}
	return &reportImpl{
		ctx.env,
		serviceConfig,
		ctx.client,
		resolver,
	}, nil
}

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

package serviceControl

import (
	"bytes"
	"fmt"
	"time"

	"github.com/pborman/uuid"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/mixer/adapter/serviceControl/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct {
		adapter.DefaultBuilder

		createClientFunc
	}

	aspect struct {
		serviceName string
		service     *sc.Service
		logger      adapter.Logger
	}
)

var (
	name        = "serviceControl"
	defaultConf = &config.Params{
		ServiceName: "library-example.sandbox.googleapis.com",
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	b := &builder{
		adapter.NewDefaultBuilder(
			name,
			"Check/report to GCP service controller",
			defaultConf,
		),
		createClient,
	}
	r.RegisterMetricsBuilder(b)
	r.RegisterApplicationLogsBuilder(b)
}

func (*builder) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) {
	params := c.(*config.Params)
	if params.ServiceName == "" {
		ce = ce.Appendf("serviceName", "empty GCP service name")
	}
	return
}

func (b *builder) NewMetricsAspect(env adapter.Env, cfg adapter.Config, _ map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	return b.newAspect(env, cfg)
}

func (b *builder) NewApplicationLogsAspect(env adapter.Env, cfg adapter.Config) (adapter.ApplicationLogsAspect, error) {
	return b.newAspect(env, cfg)
}

func (b *builder) newAspect(env adapter.Env, cfg adapter.Config) (*aspect, error) {
	params := cfg.(*config.Params)
	ss, err := b.createClientFunc(env.Logger())
	if err != nil {
		return nil, err
	}
	return &aspect{params.ServiceName, ss, env.Logger()}, nil
}

func (a *aspect) Record(values []adapter.Value) error {
	b := bytes.NewBufferString("mixer-metric-report-id-")
	_, err := b.WriteString(uuid.New())
	if err != nil {
		return err
	}
	rq := a.record(values, time.Now(), b.String())
	rp, err := a.service.Services.Report(a.serviceName, rq).Do()
	if err != nil {
		return err
	}

	if a.logger.VerbosityLevel(4) {
		a.logger.Infof("service control metric report operation id %s\nmetrics %v\nresponse %v",
			rq.Operations[0].OperationId, values, rp)
	}
	return nil
}

func (a *aspect) record(values []adapter.Value, now time.Time, operationID string) *sc.ReportRequest {
	var vs []*sc.MetricValueSet
	for _, v := range values {
		// TODO Only support request count.
		if v.Definition.Name != "request_count" {
			a.logger.Warningf("service control metric got unsupported metric name: %s\n", v.Definition.Name)
			continue
		}
		var mv sc.MetricValue
		mv.Labels, _ = translate(v.Labels)
		mv.StartTime = v.StartTime.Format(time.RFC3339Nano)
		mv.EndTime = v.EndTime.Format(time.RFC3339Nano)
		i, _ := v.Int64()
		mv.Int64Value = &i

		ms := &sc.MetricValueSet{
			MetricName:   "serviceruntime.googleapis.com/api/producer/request_count",
			MetricValues: []*sc.MetricValue{&mv},
		}
		vs = append(vs, ms)
	}

	ot := now.Format(time.RFC3339Nano)
	op := &sc.Operation{
		OperationId:     operationID,
		OperationName:   "reportMetrics",
		StartTime:       ot,
		EndTime:         ot,
		MetricValueSets: vs,
		Labels: map[string]string{
			"cloud.googleapis.com/location": "global",
		},
	}
	return &sc.ReportRequest{
		Operations: []*sc.Operation{op},
	}
}

// translate mixer label key to GCP label, and find the api method. TODO Need general redesign.
// Current implementation is just adding a prefix / which does not work for some labels.
// Need to support api_method
// "serviceruntime.googleapis.com/api_method": method,
func translate(labels map[string]interface{}) (map[string]string, string) {
	ml := make(map[string]string)
	var method string
	for k, v := range labels {
		nk := "/" + k
		ml[nk] = fmt.Sprintf("%v", v)
		if k == "request.method" {
			method = ml[nk]
		}
	}
	return ml, method
}

//TODO Add supports to struct payload.
func (a *aspect) log(entries []adapter.LogEntry, now time.Time, operationID string) *sc.ReportRequest {
	var ls []*sc.LogEntry
	for _, e := range entries {
		l := &sc.LogEntry{
			Name:        e.LogName,
			Severity:    e.Severity.String(),
			TextPayload: e.TextPayload,
			Timestamp:   e.Timestamp,
		}
		ls = append(ls, l)
	}

	ot := now.Format(time.RFC3339Nano)
	op := &sc.Operation{
		OperationId:   operationID,
		OperationName: "reportLogs",
		StartTime:     ot,
		EndTime:       ot,
		LogEntries:    ls,
		Labels:        map[string]string{"cloud.googleapis.com/location": "global"},
	}

	return &sc.ReportRequest{
		Operations: []*sc.Operation{op},
	}
}

func (a *aspect) Log(entries []adapter.LogEntry) error {
	b := bytes.NewBufferString("mixer-metric-log-id-")
	_, err := b.WriteString(uuid.New())
	if err != nil {
		return err
	}
	rq := a.log(entries, time.Now(), b.String())
	rp, err := a.service.Services.Report(a.serviceName, rq).Do()
	if err != nil {
		return err
	}

	if a.logger.VerbosityLevel(4) {
		a.logger.Infof("service control log report for operation id %s\nlogs %v\nresponse %v",
			rq.Operations[0].OperationId, entries, rp)
	}
	return nil
}

func (a *aspect) LogAccess(entries []adapter.LogEntry) error {
	//TODO access log will be merged into application log.
	return nil
}

func (a *aspect) Close() error { return nil }

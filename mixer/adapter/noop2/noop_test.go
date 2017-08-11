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

package noop2

// NOTE: This test will eventually be auto-generated so that it automatically supports all templates
//       known to Mixer. For now, it's manually curated.

import (
	"testing"
	"time"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/template/checknothing"
	"istio.io/mixer/template/listentry"
	"istio.io/mixer/template/logentry"
	"istio.io/mixer/template/metric"
	"istio.io/mixer/template/quota"
	"istio.io/mixer/template/reportnothing"
)

func TestBasic(t *testing.T) {
	info := GetBuilderInfo()

	if !contains(info.SupportedTemplates, checknothing.TemplateName) ||
		!contains(info.SupportedTemplates, reportnothing.TemplateName) ||
		!contains(info.SupportedTemplates, listentry.TemplateName) ||
		!contains(info.SupportedTemplates, logentry.TemplateName) ||
		!contains(info.SupportedTemplates, metric.TemplateName) ||
		!contains(info.SupportedTemplates, quota.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	builder := info.CreateHandlerBuilderFn()
	cfg := info.DefaultConfig

	if err := info.ValidateConfig(cfg); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	checkNothingBuilder := builder.(checknothing.CheckNothingHandlerBuilder)
	if err := checkNothingBuilder.ConfigureCheckNothingHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	reportNothingBuilder := builder.(reportnothing.ReportNothingHandlerBuilder)
	if err := reportNothingBuilder.ConfigureReportNothingHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	listEntryBuilder := builder.(listentry.ListEntryHandlerBuilder)
	if err := listEntryBuilder.ConfigureListEntryHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	logEntryBuilder := builder.(logentry.LogEntryHandlerBuilder)
	if err := logEntryBuilder.ConfigureLogEntryHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	metricBuilder := builder.(metric.MetricHandlerBuilder)
	if err := metricBuilder.ConfigureMetricHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	quotaBuilder := builder.(quota.QuotaHandlerBuilder)
	if err := quotaBuilder.ConfigureQuotaHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := builder.Build(nil, nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	checkNothingHandler := handler.(checknothing.CheckNothingHandler)
	result, caching, err := checkNothingHandler.HandleCheckNothing(nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if !result {
		t.Errorf("Got false, expecting true result")
	}

	if caching.ValidUseCount < 1000 {
		t.Errorf("Got use count of %d, expecting at least 1000", caching.ValidUseCount)
	}

	if caching.ValidDuration < 1000*time.Second {
		t.Errorf("Got duration of %v, expecting at least 1000 seconds", caching.ValidDuration)
	}

	reportNothingHandler := handler.(reportnothing.ReportNothingHandler)
	if err = reportNothingHandler.HandleReportNothing(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	listEntryHandler := handler.(listentry.ListEntryHandler)
	result, caching, err = listEntryHandler.HandleListEntry(nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if !result {
		t.Errorf("Got false, expecting true result")
	}

	if caching.ValidUseCount < 1000 {
		t.Errorf("Got use count of %d, expecting at least 1000", caching.ValidUseCount)
	}

	if caching.ValidDuration < 1000*time.Second {
		t.Errorf("Got duration of %v, expecting at least 1000 seconds", caching.ValidDuration)
	}

	logEntryHandler := handler.(logentry.LogEntryHandler)
	if err = logEntryHandler.HandleLogEntry(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	metricHandler := handler.(metric.MetricHandler)
	if err = metricHandler.HandleMetric(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	quotaHandler := handler.(quota.QuotaHandler)
	qr, caching, err := quotaHandler.HandleQuota(nil, adapter.QuotaRequestArgs{QuotaAmount: 100})
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if qr.Amount != 100 {
		t.Errorf("Got %d quota, expecting 100", qr.Amount)
	}

	if caching.ValidUseCount < 1000 {
		t.Errorf("Got use count of %d, expecting at least 1000", caching.ValidUseCount)
	}

	if caching.ValidDuration < 1000*time.Second {
		t.Errorf("Got duration of %v, expecting at least 1000 seconds", caching.ValidDuration)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

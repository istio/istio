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

package metrics

import (
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

var (
	typeTag      = monitoring.CreateLabel("type")
	formatTag    = monitoring.CreateLabel("format")
	downgradeTag = monitoring.CreateLabel("downgrade")

	// StartupTime measures the time it takes for the agent to get ready Note: This
	// is dependent on readiness probes. This means our granularity is correlated to
	// the probing interval.
	startupTime = monitoring.NewGauge(
		"startup_duration_seconds",
		"The time from the process starting to being marked ready.",
	)

	// scrapeErrors records total number of failed scrapes.
	scrapeErrors = monitoring.NewSum(
		"scrape_failures_total",
		"The total number of failed scrapes.",
	)
	EnvoyScrapeErrors = scrapeErrors.With(typeTag.Value(ScrapeTypeEnvoy))
	AppScrapeErrors   = scrapeErrors.With(typeTag.Value(ScrapeTypeApp))
	AgentScrapeErrors = scrapeErrors.With(typeTag.Value(ScrapeTypeAgent))

	// ScrapeTotals records total number of scrapes.
	ScrapeTotals = monitoring.NewSum(
		"scrapes_total",
		"The total number of scrapes.",
	)

	// scrapeFormats records total number of scrapes broken down by the exposition
	// format that the merger ultimately delivered to the scraper. The "downgrade"
	// label is "true" when the client requested protobuf (typically because
	// native Prometheus histograms are required) but the merger had to emit text
	// because at least one merged upstream did not produce protobuf, and "false"
	// otherwise. The counter is incremented exactly once per scrape.
	scrapeFormats = monitoring.NewSum(
		"scrape_format_total",
		"The total number of scrapes broken down by delivered exposition format.",
	)
	ScrapeFormatText       = scrapeFormats.With(formatTag.Value(ScrapeFormatLabelText), downgradeTag.Value("false"))
	ScrapeFormatOpenMetric = scrapeFormats.With(formatTag.Value(ScrapeFormatLabelOpenMetrics), downgradeTag.Value("false"))
	ScrapeFormatProto      = scrapeFormats.With(formatTag.Value(ScrapeFormatLabelProto), downgradeTag.Value("false"))
	ScrapeFormatDowngraded = scrapeFormats.With(formatTag.Value(ScrapeFormatLabelText), downgradeTag.Value("true"))
)

var (
	ScrapeTypeEnvoy = "envoy"
	ScrapeTypeApp   = "application"
	ScrapeTypeAgent = "agent"

	ScrapeFormatLabelText        = "text"
	ScrapeFormatLabelOpenMetrics = "openmetrics"
	ScrapeFormatLabelProto       = "proto"
)

var processStartTime = time.Now()

func RecordStartupTime() {
	delta := time.Since(processStartTime)
	startupTime.Record(delta.Seconds())
	log.Infof("Readiness succeeded in %v", delta)
}

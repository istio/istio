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

package status

import "istio.io/pkg/monitoring"

var (
	typeTag = monitoring.MustCreateLabel("type")

	// scrapeErrors records total number of failed scrapes.
	scrapeErrors = monitoring.NewSum(
		"scrape_failures_total",
		"The total number of failed scrapes.",
		monitoring.WithLabels(typeTag),
	)
	envoyScrapeErrors = scrapeErrors.With(typeTag.Value(ScrapeTypeEnvoy))
	appScrapeErrors   = scrapeErrors.With(typeTag.Value(ScrapeTypeApp))
	agentScrapeErrors = scrapeErrors.With(typeTag.Value(ScrapeTypeAgent))

	// scrapeErrors records total number of scrapes.
	scrapeTotals = monitoring.NewSum(
		"scrapes_total",
		"The total number of scrapes.",
	)
)

var (
	ScrapeTypeEnvoy = "envoy"
	ScrapeTypeApp   = "application"
	ScrapeTypeAgent = "agent"
)

func init() {
	monitoring.MustRegister(
		scrapeTotals,
		scrapeErrors,
	)
}

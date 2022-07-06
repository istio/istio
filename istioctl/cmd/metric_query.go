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

package cmd

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"
)

type Labels []string

type prometheusQueryParams struct {
	LabelSelector labels.Selector
	ByLabels      Labels
	Duration      string
}

func (b *prometheusQueryParams) ByLabelsString() string {
	bStr := b.ByLabels.String()
	if bStr == "" {
		return ""
	}
	return fmt.Sprintf(" by (%s)", bStr)
}

func (b *prometheusQueryParams) LabelSelectorString() string {
	if len(b.LabelSelector) == 0 {
		return ""
	}
	str := ""

	for i, m := range b.LabelSelector {
		if i < len(b.LabelSelector)-1 {
			str += m.String() + ","
		} else {
			str += m.String()
		}
	}
	return str
}

func (b *prometheusQueryParams) AddLabelMatcher(ls *labels.Matcher) *prometheusQueryParams {
	if ls == nil {
		return b
	}
	if b.LabelSelector == nil {
		b.LabelSelector = labels.Selector{}
	}
	b.LabelSelector = append(b.LabelSelector, ls)
	return b
}

func (b *prometheusQueryParams) AddLabelSelector(ls labels.Selector) *prometheusQueryParams {
	if b.LabelSelector == nil {
		b.LabelSelector = labels.Selector{}
	}
	b.LabelSelector = append(b.LabelSelector, ls...)
	return b
}

func (b *prometheusQueryParams) AddByLabels(ls ...string) *prometheusQueryParams {
	for _, l := range ls {
		b.AddByLabel(l)
	}
	return b
}

func (b *prometheusQueryParams) AddByLabel(l string) *prometheusQueryParams {
	if b.ByLabels == nil {
		b.ByLabels = Labels{}
	}
	if !b.ByLabels.Has(l) {
		b.ByLabels = append(b.ByLabels, l)
	}
	return b
}

// Has returns true if the label with the given name is present.
func (ls Labels) Has(name string) bool {
	for _, l := range ls {
		if l == name {
			return true
		}
	}
	return false
}

func (ls Labels) String() string {
	str := ""
	for i, l := range ls {
		if i < len(ls)-1 {
			str += l + ","
		} else {
			str += l
		}
	}
	return str
}

// common example:
//	histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s])) by (le))
//	quantile, reqDur, destWorkloadLabel, workloadName, destWorkloadNamespaceLabel, workloadNamespace, duration
func getLatencyQuery(quantile float64, queryParams prometheusQueryParams) string {
	latencyQueryTemplate := `histogram_quantile(%f, sum(rate(%s_bucket{%s}[%s]))%s)`
	return fmt.Sprintf(latencyQueryTemplate, quantile, reqDur, queryParams.LabelSelectorString(), queryParams.Duration, queryParams.ByLabelsString())
}

// common RPS:
// 	sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s]))
// 	reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration
// error RPS:
// 	sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination",response_code=~"[45][0-9]{2}"}[%s]))
//  reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration
func getRpsQuery(queryParams prometheusQueryParams) string {
	rpsQueryTemplate := `sum(rate(%s{%s}[%s]))%s`
	return fmt.Sprintf(rpsQueryTemplate, reqTot, queryParams.LabelSelectorString(), queryParams.Duration, queryParams.ByLabelsString())
}

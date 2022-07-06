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

type LabelsBuilder struct {
	LabelSelector labels.Selector
	ByLabels      Labels
	Dur           string
}

func (b *LabelsBuilder) ByLabelsString() string {
	bStr := b.ByLabels.String()
	if bStr == "" {
		return ""
	}
	return fmt.Sprintf(" by (%s)", bStr)
}

func (b *LabelsBuilder) LabelSelectorString() string {
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

func (b *LabelsBuilder) AddLabelMatcher(ls *labels.Matcher) *LabelsBuilder {
	if ls == nil {
		return b
	}
	if b.LabelSelector == nil {
		b.LabelSelector = labels.Selector{}
	}
	b.LabelSelector = append(b.LabelSelector, ls)
	return b
}

func (b *LabelsBuilder) AddLabelSelector(ls labels.Selector) *LabelsBuilder {
	if b.LabelSelector == nil {
		b.LabelSelector = labels.Selector{}
	}
	b.LabelSelector = append(b.LabelSelector, ls...)
	return b
}

func (b *LabelsBuilder) AddByLabels(ls ...string) *LabelsBuilder {
	for _, l := range ls {
		b.AddByLabel(l)
	}
	return b
}

func (b *LabelsBuilder) AddByLabel(l string) *LabelsBuilder {
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

type p8sQuantile float64

const (
	quantile50 p8sQuantile = 0.5
	quantile90 p8sQuantile = 0.9
	quantile99 p8sQuantile = 0.99
)

// common example:
//	histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s])) by (le))
//	quantile, reqDur, destWorkloadLabel, workloadName, destWorkloadNamespaceLabel, workloadNamespace, duration
func getLatencyQuery(quantile p8sQuantile, builder LabelsBuilder) string {
	latencyQueryTemplate := `histogram_quantile(%f, sum(rate(%s_bucket{%s}[%s]))%s)`
	return fmt.Sprintf(latencyQueryTemplate, quantile, reqDur, builder.LabelSelectorString(), builder.Dur, builder.ByLabelsString())
}

// common RPS:
// 	sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s]))
// 	reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration
// error RPS:
// 	sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination",response_code=~"[45][0-9]{2}"}[%s]))
//  reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration
func getRpsQuery(builder LabelsBuilder) string {
	rpsQueryTemplate := `sum(rate(%s{%s}[%s]))%s`
	return fmt.Sprintf(rpsQueryTemplate, reqTot, builder.LabelSelectorString(), builder.Dur, builder.ByLabelsString())
}

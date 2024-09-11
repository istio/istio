// Copyright 2019 Istio Authors
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

package driver

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/tests/envoye2e/env"
)

type Stats struct {
	AdminPort uint16
	Matchers  map[string]StatMatcher
}

type StatMatcher interface {
	Matches(*Params, *dto.MetricFamily) error
}

var _ Step = &Stats{}

func (s *Stats) Run(p *Params) error {
	var metrics map[string]*dto.MetricFamily
	for i := 0; i < 15; i++ {
		_, body, err := env.HTTPGet(fmt.Sprintf("http://127.0.0.1:%d/stats/prometheus", s.AdminPort))
		if err != nil {
			return err
		}
		reader := strings.NewReader(body)
		metrics, err = (&expfmt.TextParser{}).TextToMetricFamilies(reader)
		if err != nil {
			return err
		}
		count := 0
		for _, metric := range metrics {
			matcher, found := s.Matchers[metric.GetName()]
			if !found {
				continue
			}
			if err = matcher.Matches(p, metric); err == nil {
				log.Printf("matched metric %q", metric.GetName())
				count++
				continue
			} else if _, ok := matcher.(*MissingStat); ok {
				return fmt.Errorf("found metric that should have been missing: %s", metric.GetName())
			}
			log.Printf("metric %q did not match: %v\n", metric.GetName(), err)
		}
		if count == len(s.Matchers) {
			return nil
		}
		missingCount := 0
		for _, m := range s.Matchers {
			if _, ok := m.(*MissingStat); ok {
				missingCount++
			}
		}
		if count+missingCount == len(s.Matchers) {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("failed to match all metrics: want %v, but got %v", s.Matchers, metrics)
}

func (s *Stats) Cleanup() {}

type ExactStat struct {
	Metric string
}

func (me *ExactStat) Matches(params *Params, that *dto.MetricFamily) error {
	metric := &dto.MetricFamily{}
	params.LoadTestProto(me.Metric, metric)

	if diff := cmp.Diff(metric, that, protocmp.Transform()); diff != "" {
		return fmt.Errorf("diff: %v, got: %v, want: %v", diff, that, metric)
	}
	return nil
}

var _ StatMatcher = &ExactStat{}

// ExistStat matches if the metric exists in the output,
// but does not compare the Counter.
type ExistStat struct {
	Metric string
}

func (me *ExistStat) Matches(params *Params, that *dto.MetricFamily) error {
	metric := &dto.MetricFamily{}
	params.LoadTestProto(me.Metric, metric)

	switch metric.Type {
	case dto.MetricType_COUNTER.Enum():
		if diff := cmp.Diff(metric, that, protocmp.Transform(), cmpopts.IgnoreFields(dto.Counter{}, "value")); diff != "" {
			return fmt.Errorf("diff: %v, got: %v, want: %v", diff, that, metric)
		}
	}

	return nil
}

var _ StatMatcher = &ExistStat{}

type PartialStat struct {
	Metric string
}

func (me *PartialStat) Matches(params *Params, that *dto.MetricFamily) error {
	metric := &dto.MetricFamily{}
	params.LoadTestProto(me.Metric, metric)
	for _, wm := range metric.Metric {
		found := false
		for _, gm := range that.Metric {
			if diff := cmp.Diff(wm, gm, protocmp.Transform()); diff != "" {
				continue
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("cannot find metric, got: %v, want: %v", that.Metric, wm)
		}
	}
	return nil
}

var _ StatMatcher = &PartialStat{}

type MissingStat struct {
	Metric string
}

func (m *MissingStat) Matches(_ *Params, that *dto.MetricFamily) error {
	log.Printf("names: %s", *that.Name)

	if strings.EqualFold(m.Metric, *that.Name) {
		return fmt.Errorf("found metric that should be missing: %s", m.Metric)
	}
	return nil
}

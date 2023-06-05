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

package monitoring_test

import (
	"errors"
	"strings"
	"testing"

	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/monitoring/monitortest"
)

var (
	name = monitoring.MustCreateLabel("name")
	kind = monitoring.MustCreateLabel("kind")

	testSum = monitoring.NewSum(
		"events_total",
		"Number of events observed, by name and kind",
		monitoring.WithLabels(name, kind),
	)

	goofySum = testSum.With(kind.Value("goofy"))

	hookSum = monitoring.NewSum(
		"hook_total",
		"Number of hook events observed",
		monitoring.WithLabels(name),
	)

	testDistribution = monitoring.NewDistribution(
		"test_buckets",
		"Testing distribution functionality",
		[]float64{0, 2.5, 7, 8, 10, 154.3, 99},
		monitoring.WithLabels(name),
		monitoring.WithUnit(monitoring.Seconds),
	)

	testGauge = monitoring.NewGauge(
		"test_gauge",
		"Testing gauge functionality",
	)

	testDisabledSum = monitoring.NewSum(
		"events_disabled_total",
		"Number of events observed, by name and kind",
		monitoring.WithLabels(name, kind),
	)

	testConditionalSum = monitoring.NewSum(
		"events_conditional_total",
		"Number of events observed, by name and kind",
		monitoring.WithLabels(name, kind),
	)
)

func init() {
	monitoring.MustRegister(testSum, hookSum, testDistribution, testGauge)
	testDisabledSum = monitoring.RegisterIf(testDisabledSum, func() bool { return false })
	testConditionalSum = monitoring.RegisterIf(testConditionalSum, func() bool { return true })
}

func TestSum(t *testing.T) {
	mt := monitortest.New(t)

	testSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	goofySum.With(name.Value("baz")).Record(45)
	goofySum.With(name.Value("baz")).Decrement() // should use floor, so this will be counted as 10

	mt.Assert(goofySum.Name(), map[string]string{"name": "baz"}, monitortest.Exactly(44))
	mt.Assert(testSum.Name(), map[string]string{"kind": "bar"}, monitortest.Exactly(1))
}

func TestRegisterIfSum(t *testing.T) {
	testDisabledSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	var err error
	_, err = view.RetrieveData(testDisabledSum.Name())
	if err == nil || !strings.EqualFold(err.Error(), "cannot retrieve data; view \"events_disabled_total\" is not registered") {
		t.Errorf("failure validating disabled metrics. exptected error but got %v", err)
	}
	testConditionalSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	rows, err := view.RetrieveData(testConditionalSum.Name())
	if err != nil {
		t.Errorf("exptected to register metric %s but got %v", testConditionalSum.Name(), err)
	}
	if len(rows) == 0 {
		t.Errorf("exptected to metric %s has values  but got %v", testConditionalSum.Name(), len(rows))
	}
}

func TestGauge(t *testing.T) {
	mt := monitortest.New(t)

	testGauge.Record(42)
	testGauge.Record(77)

	mt.Assert(testGauge.Name(), nil, monitortest.Exactly(77))
}

func TestDerivedGauge(t *testing.T) {
	testDerivedGauge := monitoring.NewDerivedGauge(
		"test_derived_gauge",
		"Testing derived gauge functionality",
		monitoring.WithLabelKeys("blah"), // NOTE: This will be ignored!
		monitoring.WithValueFrom(
			func() float64 {
				return 17.76
			},
		),
	)
	mt := monitortest.New(t)
	mt.Assert(testDerivedGauge.Name(), nil, monitortest.Exactly(17.76))
}

func TestDerivedGaugeWithLabels(t *testing.T) {
	testDerivedGauge := monitoring.NewDerivedGauge(
		"test_derived_gauge",
		"Testing derived gauge functionality",
		monitoring.WithLabelKeys("foo"),
	)

	testDerivedGauge.ValueFrom(
		func() float64 {
			return 17.76
		},
		"bar",
	)

	testDerivedGauge.ValueFrom(
		func() float64 {
			return 18.12
		},
		"baz",
	)

	mt := monitortest.New(t)

	cases := []struct {
		wantLabel string
		wantValue float64
	}{
		{"bar", 17.76},
		{"baz", 18.12},
	}
	for _, tc := range cases {
		t.Run(tc.wantLabel, func(tt *testing.T) {
			mt.Assert(testDerivedGauge.Name(), map[string]string{"foo": tc.wantLabel}, monitortest.Exactly(tc.wantValue))
		})
	}
}

func TestDistribution(t *testing.T) {
	mt := monitortest.New(t)

	funDistribution := testDistribution.With(name.Value("fun"))
	funDistribution.Record(7.7773)
	testDistribution.With(name.Value("foo")).Record(7.4)
	testDistribution.With(name.Value("foo")).Record(6.8)
	testDistribution.With(name.Value("foo")).Record(10.2)

	mt.Assert(testDistribution.Name(), map[string]string{"name": "fun"}, monitortest.Distribution(1, 7.7773))
	mt.Assert(testDistribution.Name(), map[string]string{"name": "foo"}, monitortest.Distribution(3, 24.4))
}

func TestMustCreateLabel(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			if !strings.Contains(r.(error).Error(), "label") {
				t.Errorf("no panic for invalid label, recovered: %q", r.(error).Error())
			}
		} else {
			t.Error("no panic for failed label creation.")
		}
	}()

	// labels must be ascii
	monitoring.MustCreateLabel("£®")
}

func TestMustRegister(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("no panic for failed registration.")
		}
	}()

	monitoring.MustRegister(&registerFail{})
}

type registerFail struct {
	monitoring.Metric
}

func (r registerFail) Register() error {
	return errors.New("fail")
}

func BenchmarkCounter(b *testing.B) {
	monitortest.New(b)
	for n := 0; n < b.N; n++ {
		testSum.Increment()
	}
}

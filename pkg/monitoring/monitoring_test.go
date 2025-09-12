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
	"fmt"
	"testing"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

var (
	name = monitoring.CreateLabel("name")
	kind = monitoring.CreateLabel("kind")

	testSum = monitoring.NewSum(
		"events_total",
		"Number of events observed, by name and kind",
	)

	goofySum = testSum.With(kind.Value("goofy"))

	hookSum = monitoring.NewSum(
		"hook_total",
		"Number of hook events observed",
	)

	testDistribution = monitoring.NewDistribution(
		"test_buckets",
		"Testing distribution functionality",
		[]float64{0, 2.5, 7, 8, 10, 99, 154.3},
		monitoring.WithUnit(monitoring.Seconds),
	)

	testGauge = monitoring.NewGauge(
		"test_gauge",
		"Testing gauge functionality",
	)

	testDisabledSum = monitoring.NewSum(
		"events_disabled_total",
		"Number of events observed, by name and kind",
		monitoring.WithEnabled(func() bool { return false }),
	)

	testConditionalSum = monitoring.NewSum(
		"events_conditional_total",
		"Number of events observed, by name and kind",
		monitoring.WithEnabled(func() bool { return true }),
	)
)

func TestMonitorTestReset(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		mt := monitortest.New(t)
		testSum.With(name.Value("foo"), kind.Value("bar")).Increment()
		mt.Assert(testSum.Name(), map[string]string{"kind": "bar"}, monitortest.Exactly(1))
	})
	t.Run("secondary", func(t *testing.T) {
		mt := monitortest.New(t)
		testSum.With(name.Value("foo"), kind.Value("bar2")).Increment()
		// Should have been reset
		mt.Assert(testSum.Name(), map[string]string{"kind": "bar"}, monitortest.Exactly(0))
		mt.Assert(testSum.Name(), map[string]string{"kind": "bar2"}, monitortest.Exactly(1))
	})
}

func TestSum(t *testing.T) {
	mt := monitortest.New(t)

	testSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	goofySum.With(name.Value("baz")).Record(45)
	goofySum.With(name.Value("baz")).Decrement()

	mt.Assert(goofySum.Name(), map[string]string{"kind": "goofy", "name": "baz"}, monitortest.Exactly(44))
	mt.Assert(testSum.Name(), map[string]string{"kind": "bar"}, monitortest.Exactly(1))
}

func TestRegisterIfSum(t *testing.T) {
	mt := monitortest.New(t)

	testDisabledSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	mt.Assert(testDisabledSum.Name(), nil, monitortest.DoesNotExist)

	testConditionalSum.With(name.Value("foo"), kind.Value("bar")).Increment()
	mt.Assert(testConditionalSum.Name(), map[string]string{"name": "foo", "kind": "bar"}, monitortest.Exactly(1))
}

// Create distinct metrics for this test, otherwise we are order-dependent since they are globals
var (
	testEmptyGauge = monitoring.NewGauge(
		"test_empty_gauge",
		"Testing empty gauge functionality",
	)
	testEmptySum = monitoring.NewSum(
		"test_empty_sum",
		"Testing empty sum",
	)
	testEmptyDistribution = monitoring.NewDistribution(
		"test_empty_dist",
		"Testing empty dist",
		[]float64{0, 1, 2},
	)
)

func TestEmptyMetrics(t *testing.T) {
	mt := monitortest.New(t)
	relevantMetrics := sets.New(testEmptyGauge.Name(), testEmptySum.Name(), testEmptyDistribution.Name())
	assertRecordedMetrics := func(names ...string) {
		t.Helper()
		want := sets.New(names...)
		got := sets.New(slices.Map(mt.Metrics(), func(e monitortest.Metric) string {
			return e.Name
		})...).Intersection(relevantMetrics)
		assert.Equal(t, want, got)
	}
	// derived gauge is always present
	assertRecordedMetrics()

	// Once we write it shows up
	testEmptyGauge.Record(0)
	testEmptySum.Record(0)
	testEmptyDistribution.Record(0)
	assertRecordedMetrics(relevantMetrics.UnsortedList()...)
}

func TestGauge(t *testing.T) {
	mt := monitortest.New(t)

	testGauge.Record(42)
	testGauge.Record(77)

	mt.Assert(testGauge.Name(), nil, monitortest.Exactly(77))
}

func TestGaugeLabels(t *testing.T) {
	mt := monitortest.New(t)

	testGauge.With(kind.Value("foo")).Record(42)
	testGauge.With(kind.Value("bar")).Record(77)
	testGauge.With(kind.Value("bar")).Record(72)

	mt.Assert(testGauge.Name(), map[string]string{"kind": "foo"}, monitortest.Exactly(42))
	mt.Assert(testGauge.Name(), map[string]string{"kind": "bar"}, monitortest.Exactly(72))
}

// Regression test to ensure we can safely mutate labels in parallel
func TestParallelLabels(t *testing.T) {
	metrics := []monitoring.Metric{testGauge, testDistribution, testSum}
	for _, m := range metrics {
		for i := range 100 {
			go func() {
				m.With(kind.Value(fmt.Sprint(i))).Record(1)
			}()
		}
	}
}

var (
	testDerivedGauge = monitoring.NewDerivedGauge(
		"test_derived_gauge",
		"Testing derived gauge functionality",
	).ValueFrom(func() float64 {
		return 17.76
	})

	testDerivedGaugeLabels = monitoring.NewDerivedGauge(
		"test_derived_gauge_labels",
		"Testing derived gauge functionality",
	)
)

func TestDerivedGauge(t *testing.T) {
	mt := monitortest.New(t)
	mt.Assert(testDerivedGauge.Name(), nil, monitortest.Exactly(17.76))
}

func TestDerivedGaugeWithLabels(t *testing.T) {
	foo := monitoring.CreateLabel("foo")
	testDerivedGaugeLabels.ValueFrom(
		func() float64 {
			return 17.76
		},
		foo.Value("bar"),
	)

	testDerivedGaugeLabels.ValueFrom(
		func() float64 {
			return 18.12
		},
		foo.Value("baz"),
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
			mt.Assert(testDerivedGaugeLabels.Name(), map[string]string{"foo": tc.wantLabel}, monitortest.Exactly(tc.wantValue))
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
	mt.Assert(testDistribution.Name(), map[string]string{"name": "foo"}, monitortest.Buckets(7))
}

func TestRecordHook(t *testing.T) {
	mt := monitortest.New(t)

	// testRecordHook will record value for hookSum measure when testSum is recorded
	rh := &testRecordHook{}
	monitoring.RegisterRecordHook(testSum.Name(), rh)

	testSum.With(name.Value("foo"), kind.Value("bart")).Increment()
	testSum.With(name.Value("baz"), kind.Value("bart")).Record(45)

	mt.Assert(testSum.Name(), map[string]string{"name": "foo", "kind": "bart"}, monitortest.Exactly(1))
	mt.Assert(testSum.Name(), map[string]string{"name": "baz", "kind": "bart"}, monitortest.Exactly(45))
	mt.Assert(hookSum.Name(), map[string]string{"name": "foo"}, monitortest.Exactly(1))
	mt.Assert(hookSum.Name(), map[string]string{"name": "baz"}, monitortest.Exactly(45))
}

type testRecordHook struct{}

func (r *testRecordHook) OnRecord(n string, tags []monitoring.LabelValue, value float64) {
	// Check if this is `events_total` metric.
	if n != "events_total" {
		return
	}

	// Get name tag of recorded testSume metric, and record the corresponding hookSum metric.
	var nv string
	for _, tag := range tags {
		if tag.Key() == name {
			nv = tag.Value()
			break
		}
	}
	hookSum.With(name.Value(nv)).Record(value)
}

func BenchmarkCounter(b *testing.B) {
	monitortest.New(b)
	b.Run("no labels", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			testSum.Increment()
		}
	})
	b.Run("dynamic labels", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			testSum.With(name.Value("test")).Increment()
		}
	})
	b.Run("static labels", func(b *testing.B) {
		testSum := testSum.With(name.Value("test"))
		for n := 0; n < b.N; n++ {
			testSum.Increment()
		}
	})
}

func BenchmarkGauge(b *testing.B) {
	monitortest.New(b)
	b.Run("no labels", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			testGauge.Increment()
		}
	})
	b.Run("dynamic labels", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			testGauge.With(name.Value("test")).Increment()
		}
	})
	b.Run("static labels", func(b *testing.B) {
		testGauge := testGauge.With(name.Value("test"))
		for n := 0; n < b.N; n++ {
			testGauge.Increment()
		}
	})
}

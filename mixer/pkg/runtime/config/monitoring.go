// Copyright 2018 Istio Authors
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

package config

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	configID = "configID"
)

var (
	standardConfigLabels = []string{configID}

	attributeCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "attribute_count",
		Help:      "The number of known attributes in the current config.",
	}, standardConfigLabels)

	handlerConfigCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "handler_config_count",
		Help:      "The number of known handlers in the current config.",
	}, standardConfigLabels)

	instanceConfigCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "instance_config_count",
		Help:      "The number of known instances in the current config.",
	}, standardConfigLabels)

	ruleConfigCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "rule_config_count",
		Help:      "The number of known instances in the current config.",
	}, standardConfigLabels)

	ruleConfigErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "rule_config_error_count",
		Help:      "The number of errors encountered during processing of the rule configuration.",
	}, standardConfigLabels)

	matchErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "rule_config_match_error_count",
		Help:      "The number of rule conditions that was not parseable.",
	}, standardConfigLabels)

	unsatisfiedActionHandlerCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mixer",
		Subsystem: "config",
		Name:      "unsatisfied_action_handler_count",
		Help:      "The number of actions that were put into action due to handlers being unavailable.",
	}, standardConfigLabels)
)

func init() {
	prometheus.MustRegister(attributeCount)
	prometheus.MustRegister(handlerConfigCount)
	prometheus.MustRegister(instanceConfigCount)
	prometheus.MustRegister(ruleConfigCount)
	prometheus.MustRegister(ruleConfigErrorCount)
	prometheus.MustRegister(matchErrorCount)
	prometheus.MustRegister(unsatisfiedActionHandlerCount)
}

// Counters is the configuration related performance Counters. Other parts of the code can depend
// on some of the Counters here as well.
type Counters struct {
	attributes      prometheus.Counter
	handlerConfig   prometheus.Counter
	instanceConfig  prometheus.Counter
	ruleConfig      prometheus.Counter
	ruleConfigError prometheus.Counter

	// Externally visible counters
	MatchErrors               prometheus.Counter
	UnsatisfiedActionHandlers prometheus.Counter
}

func newCounters(id int64) Counters {
	labels := prometheus.Labels{
		configID: strconv.FormatInt(id, 10),
	}
	return Counters{
		attributes:      attributeCount.With(labels),
		handlerConfig:   handlerConfigCount.With(labels),
		instanceConfig:  instanceConfigCount.With(labels),
		ruleConfig:      ruleConfigCount.With(labels),
		ruleConfigError: ruleConfigErrorCount.With(labels),

		MatchErrors:               matchErrorCount.With(labels),
		UnsatisfiedActionHandlers: unsatisfiedActionHandlerCount.With(labels),
	}
}

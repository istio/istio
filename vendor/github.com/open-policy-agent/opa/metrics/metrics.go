// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package metrics contains helpers for performance metric management inside the policy engine.
package metrics

import (
	"time"
)

// Well-known metric names.
const (
	RegoQueryCompile  = "rego_query_compile"
	RegoQueryEval     = "rego_query_eval"
	RegoQueryParse    = "rego_query_parse"
	RegoModuleParse   = "rego_module_parse"
	RegoModuleCompile = "rego_module_compile"
)

// Metrics defines the interface for a collection of perfomrance metrics in the
// policy engine.
type Metrics interface {
	Timer(name string) Timer
	All() map[string]interface{}
	Clear()
}

type metrics struct {
	timers map[string]Timer
}

// Sample defines a common interface to obtain a single metric value.
type Sample interface {
	Value() interface{}
}

// New returns a new Metrics object.
func New() Metrics {
	return &metrics{
		timers: map[string]Timer{},
	}
}

func (m *metrics) Timer(name string) Timer {
	t, ok := m.timers[name]
	if !ok {
		t = &timer{}
		m.timers[name] = t
	}
	return t
}

func (m *metrics) All() map[string]interface{} {
	result := map[string]interface{}{}
	for name, timer := range m.timers {
		result[m.formatKey(name, timer)] = timer.Value()
	}
	return result
}

func (m *metrics) Clear() {
	m.timers = map[string]Timer{}
}

func (m *metrics) formatKey(name string, sample Sample) string {
	switch sample.(type) {
	case Timer:
		return "timer_" + name + "_ns"
	default:
		return name
	}
}

// Timer defines the interface for a restartable timer that accumulates elapsed
// time.
type Timer interface {
	Sample
	Start()
	Stop()
}

type timer struct {
	start time.Time
	value int64
}

func (t *timer) Start() {
	t.start = time.Now()
}

func (t *timer) Stop() {
	t.value += time.Now().Sub(t.start).Nanoseconds()
}

func (t timer) Value() interface{} {
	return t.value
}

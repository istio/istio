// Copyright 2017 Google Inc.
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

package memQuota

import (
	"fmt"
	"testing"
	"time"

	"istio.io/mixer/pkg/adapter"

	"istio.io/mixer/adapter/memQuota/config"
	at "istio.io/mixer/pkg/adapter/testing"
)

type fakeEnv struct {
	t *testing.T
}

func (e fakeEnv) Logger() adapter.Logger {
	return e
}

func (e fakeEnv) Infof(format string, args ...interface{}) {
	e.t.Logf(format, args...)
}

func (e fakeEnv) Warningf(format string, args ...interface{}) {
	e.t.Logf(format, args...)
}

func (e fakeEnv) Errorf(format string, args ...interface{}) error {
	e.t.Logf(format, args...)
	return fmt.Errorf(format, args)
}

func TestAllocAndRelese(t *testing.T) {
	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    0,
	}

	definitions["Q2"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    time.Second,
	}

	definitions["Q3"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    time.Second * 60,
	}

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.MinDeduplicationWindowSeconds = 3600

	a, err := b.NewQuota(&fakeEnv{t}, c, definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	asp := a.(*memQuota)

	cases := []struct {
		name            string
		dedup           string
		allocAmount     int64
		allocResult     int64
		allocBestEffort bool
		tick            int32
		releaseAmount   int64
		releaseResult   int64
	}{
		{"Q1", "0", 2, 2, false, 0, 0, 0},
		{"Q1", "0", 2, 2, false, 0, 0, 0}, // should be a nop due to dedup
		{"Q1", "1", 6, 6, false, 0, 0, 0},
		{"Q1", "2", 2, 2, false, 0, 0, 0},
		{"Q1", "3", 2, 0, false, 0, 0, 0},
		{"Q1", "4", 1, 0, true, 0, 0, 0},
		{"Q1", "5", 0, 0, true, 0, 0, 0},
		{"Q1", "5b", 0, 0, false, 0, 5, 5},
		{"Q1", "5b", 0, 0, false, 0, 5, 5},
		{"Q1", "5c", 0, 0, false, 0, 15, 5},

		{"Q3", "6", 10, 10, false, 1, 0, 0},
		{"Q3", "7", 10, 0, false, 2, 0, 0},
		{"Q3", "8", 10, 0, false, 60, 0, 0},
		{"Q3", "9", 10, 10, false, 61, 0, 0},
		{"Q3", "10", 100, 10, true, 121, 0, 0},
		{"Q3", "11", 100, 0, false, 122, 10, 10},
		{"Q3", "12", 0, 0, false, 123, 1000, 0},
		{"Q3", "12", 0, 0, false, 124, 1000, 0},
	}

	labels := make(map[string]interface{})
	labels["L1"] = "string"
	labels["L2"] = int64(2)
	labels["L3"] = float64(3.0)
	labels["L4"] = true
	labels["L5"] = time.Now()
	labels["L6"] = []byte{0, 1}

	for i, c := range cases {
		qa := adapter.QuotaArgs{
			Name:            c.name,
			DeduplicationID: "A" + c.dedup,
			QuotaAmount:     c.allocAmount,
			Labels:          labels,
		}

		asp.getTick = func() int32 {
			return c.tick * ticksPerSecond
		}

		var amount int64
		var err error

		if c.allocBestEffort {
			amount, err = a.AllocBestEffort(qa)
		} else {
			amount, err = a.Alloc(qa)
		}

		if err != nil {
			t.Errorf("Alloc(%v): expecting success, got %v", i, err)
		}

		if amount != c.allocResult {
			t.Errorf("Alloc(%v): expecting %d, got %d", i, c.allocResult, amount)
		}

		qa = adapter.QuotaArgs{
			Name:            c.name,
			DeduplicationID: "R" + c.dedup,
			QuotaAmount:     c.releaseAmount,
			Labels:          labels,
		}

		amount, err = a.ReleaseBestEffort(qa)
		if err != nil {
			t.Errorf("Release(%v): expecting success, got %v", i, err)
		}

		if amount != c.releaseResult {
			t.Errorf("Release(%v): expecting %d, got %d", i, c.releaseResult, amount)
		}
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadName(t *testing.T) {
	b := newBuilder()
	a, err := b.NewQuota(&fakeEnv{t}, b.DefaultConfig(), make(map[string]*adapter.QuotaDefinition))
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	qa := adapter.QuotaArgs{Name: "FOO"}

	amount, err := a.Alloc(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	amount, err = a.AllocBestEffort(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	amount, err = a.ReleaseBestEffort(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadAmount(t *testing.T) {
	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    0,
	}

	b := newBuilder()
	a, err := b.NewQuota(&fakeEnv{t}, b.DefaultConfig(), definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	qa := adapter.QuotaArgs{Name: "Q1", QuotaAmount: -1}

	amount, err := a.Alloc(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	amount, err = a.AllocBestEffort(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	amount, err = a.ReleaseBestEffort(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Errorf("Expecting error, got success")
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadConfig(t *testing.T) {
	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.MinDeduplicationWindowSeconds = 0

	if err := b.ValidateConfig(c); err == nil {
		t.Errorf("Expecting failure, got success")
	}
}

func TestReaper(t *testing.T) {
	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    0,
	}

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.MinDeduplicationWindowSeconds = 3600

	a, err := b.NewQuota(&fakeEnv{t}, c, definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	asp := a.(*memQuota)

	asp.getTick = func() int32 {
		return 1
	}

	qa := adapter.QuotaArgs{
		Name:        "Q1",
		QuotaAmount: 10,
	}

	qa.DeduplicationID = "0"
	amount, _ := asp.Alloc(qa)
	if amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", amount)
	}

	amount, _ = asp.Alloc(qa)
	if amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", amount)
	}

	qa.DeduplicationID = "1"
	amount, _ = asp.Alloc(qa)
	if amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", amount)
	}

	// move current dedup state into old dedup state
	asp.reapDedup()

	qa.DeduplicationID = "2"
	amount, _ = asp.Alloc(qa)
	if amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", amount)
	}

	// retire original dedup state
	asp.reapDedup()

	qa.DeduplicationID = "0"
	amount, _ = asp.Alloc(qa)
	if amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", amount)
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestReaperTicker(t *testing.T) {
	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount: 10,
		Window:    0,
	}

	testChan := make(chan time.Time)
	testTicker := &time.Ticker{C: testChan}
	a, err := newAspectWithDedup(&fakeEnv{t}, testTicker, definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	qa := adapter.QuotaArgs{
		Name:            "Q1",
		QuotaAmount:     10,
		DeduplicationID: "0",
	}

	amount, _ := a.Alloc(qa)
	if amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", amount)
	}

	amount, _ = a.Alloc(qa)
	if amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", amount)
	}

	// Advance 3 ticks, ensuring clearing of the de-dup cache
	testChan <- time.Now()
	testChan <- time.Now()
	testChan <- time.Now()

	qa.DeduplicationID = "1"
	amount, _ = a.Alloc(qa)
	if amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", amount)
	}

	qa.DeduplicationID = "0"
	amount, _ = a.Alloc(qa)
	if amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", amount)
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}
}

func TestInvariants(t *testing.T) {
	at.TestAdapterInvariants(Register, t)
}

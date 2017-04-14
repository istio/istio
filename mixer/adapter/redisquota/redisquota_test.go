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

// Package redisquota provides a quota implementation with redis as backend.
// The prerequisite is to have a redis server running.
package redisquota

import (
	"strconv"
	"testing"
	"time"

	"github.com/chowchow316/miniredis"

	"istio.io/mixer/adapter/redisquota/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

// TODO: add more unit tests here and try to reuse the tests with memQuota adapter
func TestAllocAndRelease(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Errorf("Unable to start mini redis: %v", err)
	}
	defer s.Close()

	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount:  10,
		Expiration: 0,
	}

	definitions["Q3"] = &adapter.QuotaDefinition{
		MaxAmount:  10,
		Expiration: time.Second * 2,
	}

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.RedisServerUrl = s.Addr()
	c.MinDeduplicationDuration = time.Duration(3600) * time.Second

	a, err := b.NewQuotasAspect(test.NewEnv(t), c, definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	asp := a.(*redisQuota)

	cases := []struct {
		name            string
		dedup           string
		allocAmount     int64
		allocResult     int64
		allocBestEffort bool
		exp             time.Duration
		seconds         int64
		releaseAmount   int64
		releaseResult   int64
	}{
		{"Q1", "0", 2, 2, false, 0, 0, 0, 0},
		{"Q1", "0", 2, 2, false, 0, 0, 0, 0}, // should be a nop due to dedup
		{"Q1", "1", 6, 6, false, 0, 0, 0, 0},
		{"Q1", "2", 2, 2, false, 0, 0, 0, 0},
		{"Q1", "4", 1, 0, true, 0, 0, 0, 0},
		{"Q1", "5", 0, 0, true, 0, 0, 0, 0},
		{"Q1", "5b", 0, 0, false, 0, 0, 5, 5},
		{"Q1", "5b", 0, 0, false, 0, 0, 5, 5},
		{"Q1", "5c", 0, 0, false, 0, 0, 15, 5},

		{"Q3", "6", 10, 10, false, time.Second * 2, 0, 0, 0},
		{"Q3", "7", 10, 0, false, 0, 1, 0, 0},
		{"Q3", "8", 10, 10, false, time.Second * 2, 3, 0, 0},
		{"Q3", "9", 100, 10, true, time.Second * 2, 5, 0, 0},
		{"Q3", "10", 10, 0, false, 0, 6, 10, 10},
		{"Q3", "11", 0, 0, false, 0, 7, 1000, 0},
		{"Q3", "11", 0, 0, false, 0, 7, 1000, 0},
	}

	labels := make(map[string]interface{})
	labels["L1"] = "string"
	labels["L2"] = int64(2)
	labels["L3"] = float64(3.0)
	labels["L4"] = true
	labels["L5"] = time.Now()
	labels["L6"] = []byte{0, 1}
	labels["L7"] = map[string]string{"Foo": "Bar"}

	now := time.Now()
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			qa := adapter.QuotaArgs{
				Definition:      definitions[c.name],
				DeduplicationID: "A" + c.dedup,
				QuotaAmount:     c.allocAmount,
				Labels:          labels,
			}

			asp.common.GetTime = func() time.Time {
				return now.Add(time.Duration(c.seconds) * time.Second)
			}

			var qr adapter.QuotaResult
			var err error

			if c.allocBestEffort {
				qr, err = a.AllocBestEffort(qa)
			} else {
				qr, err = a.Alloc(qa)
			}

			if err != nil {
				t.Errorf("Expecting success, got %v", err)
			}

			if qr.Amount != c.allocResult {
				t.Errorf("Expecting %d, got %d", c.allocResult, qr.Amount)
			}

			if qr.Expiration != c.exp {
				t.Errorf("Expecting %v, got %v", c.exp, qr.Expiration)
			}

			qa = adapter.QuotaArgs{
				Definition:      definitions[c.name],
				DeduplicationID: "R" + c.dedup,
				QuotaAmount:     c.releaseAmount,
				Labels:          labels,
			}

			var amount int64
			amount, err = a.ReleaseBestEffort(qa)
			if err != nil {
				t.Errorf("Expecting success, got %v", err)
			}

			if amount != c.releaseResult {
				t.Errorf("Expecting %d, got %d", c.releaseResult, amount)
			}
		})

		// To simulate time proceed for mock redis.
		if i == 9 || i == 12 || i == 13 {
			s.FastForward(time.Second)

		}
		if i == 10 || i == 11 {
			s.FastForward(time.Second * 2)

		}
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadConnection(t *testing.T) {
	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount:  10,
		Expiration: 0,
	}

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)

	a, err := b.NewQuotasAspect(test.NewEnv(t), c, definitions)

	if err == nil {
		t.Errorf("Expecting error, got success")
	}
	if a != nil {
		t.Errorf("Expecting failure, got success")
	}

}

func TestBadAmount(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Errorf("Unable to start mini redis: %v", err)
	}
	defer s.Close()

	definitions := make(map[string]*adapter.QuotaDefinition)
	definitions["Q1"] = &adapter.QuotaDefinition{
		MaxAmount:  10,
		Expiration: 0,
	}

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.RedisServerUrl = s.Addr()
	c.MinDeduplicationDuration = time.Duration(3600) * time.Second

	a, err := b.NewQuotasAspect(test.NewEnv(t), c, definitions)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	qa := adapter.QuotaArgs{Definition: definitions["Q1"], QuotaAmount: -1}

	qr, err := a.Alloc(qa)
	if qr.Amount != 0 {
		t.Errorf("Expected 0 amount, got %d", qr.Amount)
	}

	if err == nil {
		t.Error("Expecting error, got success")
	}

	qr, err = a.AllocBestEffort(qa)
	if qr.Amount != 0 {
		t.Errorf("Expected 0 amount, got %d", qr.Amount)
	}

	if err == nil {
		t.Error("Expecting error, got success")
	}

	var amount int64
	amount, err = a.ReleaseBestEffort(qa)
	if amount != 0 {
		t.Errorf("Expected 0 amount, got %d", amount)
	}

	if err == nil {
		t.Error("Expecting error, got success")
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadConfig(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Errorf("Unable to start mini redis: %v", err)
	}
	defer s.Close()

	b := newBuilder()
	c := b.DefaultConfig().(*config.Params)
	c.RedisServerUrl = s.Addr()

	c.MinDeduplicationDuration = 0
	if err := b.ValidateConfig(c); err == nil {
		t.Error("Expecting failure, got success")
	}

	c.MinDeduplicationDuration = time.Duration(-1)
	if err := b.ValidateConfig(c); err == nil {
		t.Error("Expecting failure, got success")
	}

	c.ConnectionPoolSize = -1
	if err := b.ValidateConfig(c); err == nil {
		t.Error("Expecting failure, got success")
	}
}

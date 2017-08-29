// Copyright 2017 Istio Authors
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

package memquota

import (
	"context"
	"strconv"
	"testing"
	"time"

	"istio.io/mixer/adapter/memquota2/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
	"istio.io/mixer/template/quota"
)

func TestBasic(t *testing.T) {
	info := GetBuilderInfo()

	if !containsQuotaTemplate(info.SupportedTemplates) {
		t.Error("Didn't find all expected supported templates")
	}

	builder := info.CreateHandlerBuilder()
	cfg := info.DefaultConfig

	if err := info.ValidateConfig(cfg); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	quotaBuilder := builder.(quota.HandlerBuilder)
	if err := quotaBuilder.ConfigureQuotaHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := builder.Build(cfg, test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func containsQuotaTemplate(s []string) bool {
	for _, a := range s {
		if a == quota.TemplateName {
			return true
		}
	}
	return false
}

func TestAllocAndRelease(t *testing.T) {
	limits := []config.Params_Quota{
		{
			Name:          "Q1",
			MaxAmount:     10,
			ValidDuration: 0,
		},

		{
			Name:          "Q2",
			MaxAmount:     10,
			ValidDuration: time.Second,
		},

		{
			Name:          "Q3",
			MaxAmount:     10,
			ValidDuration: time.Second * 60,
		},
	}

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		MinDeduplicationDuration: 3600 * time.Second,
		Quotas: limits,
	}

	hndlr, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	h := hndlr.(*handler)

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
		{"Q1", "3", 2, 0, false, 0, 0, 0, 0},
		{"Q1", "4", 1, 0, true, 0, 0, 0, 0},
		{"Q1", "5", 0, 0, true, 0, 0, 0, 0},
		{"Q1", "5b", 0, 0, false, 0, 0, 5, 5},
		{"Q1", "5b", 0, 0, false, 0, 0, 5, 5},
		{"Q1", "5c", 0, 0, false, 0, 0, 15, 5},

		{"Q3", "6", 10, 10, false, time.Second * 60, 1, 0, 0},
		{"Q3", "7", 10, 0, false, 0, 2, 0, 0},

		{"Q3", "8", 10, 0, false, 0, 60, 0, 0},
		{"Q3", "9", 10, 10, false, time.Second * 60, 61, 0, 0},

		{"Q3", "10", 100, 10, true, time.Second * 60, 121, 0, 0},
		{"Q3", "11", 100, 0, false, 0, 122, 10, 10},
		{"Q3", "12", 0, 0, false, 0, 123, 1000, 0},
		{"Q3", "12", 0, 0, false, 0, 124, 1000, 0},
	}

	dims := make(map[string]interface{})
	dims["L1"] = "string"
	dims["L2"] = int64(2)
	dims["L3"] = float64(3.0)
	dims["L4"] = true
	dims["L5"] = time.Now()
	dims["L6"] = []byte{0, 1}
	dims["L7"] = map[string]string{"Foo": "Bar"}

	now := time.Now()
	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			qa := adapter.QuotaRequestArgs{
				DeduplicationID: "A" + c.dedup,
				QuotaAmount:     c.allocAmount,
				BestEffort:      c.allocBestEffort,
			}

			instance := quota.Instance{
				Name:       c.name,
				Dimensions: dims,
			}

			h.common.getTime = func() time.Time {
				return now.Add(time.Duration(c.seconds) * time.Second)
			}

			var qr adapter.QuotaResult2
			var err error

			qr, err = h.HandleQuota(context.Background(), &instance, qa)

			if err != nil {
				t.Errorf("Expecting success, got %v", err)
			}

			if qr.Amount != c.allocResult {
				t.Errorf("Expecting %d, got %d", c.allocResult, qr.Amount)
			}

			if qr.ValidDuration != c.exp {
				t.Errorf("Expecting %v, got %v", c.exp, qr.ValidDuration)
			}

			qa = adapter.QuotaRequestArgs{
				DeduplicationID: "R" + c.dedup,
				QuotaAmount:     -c.releaseAmount,
			}

			qr, err = h.HandleQuota(context.Background(), &instance, qa)
			if err != nil {
				t.Errorf("Expecting success, got %v", err)
			}

			if qr.Amount != c.releaseResult {
				t.Errorf("Expecting %d, got %d", c.releaseResult, qr.Amount)
			}
		})
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestBadConfig(t *testing.T) {
	info := GetBuilderInfo()
	c := info.DefaultConfig.(*config.Params)

	c.MinDeduplicationDuration = 0
	if err := info.ValidateConfig(c); err == nil {
		t.Error("Expecting failure, got success")
	}

	c.MinDeduplicationDuration = -1
	if err := info.ValidateConfig(c); err == nil {
		t.Error("Expecting failure, got success")
	}

	c.MinDeduplicationDuration = 1 * time.Second

	builder := info.CreateHandlerBuilder().(*builder)

	types := map[string]*quota.Type{
		"Foo": {},
	}

	if err := builder.ConfigureQuotaHandler(types); err != nil {
		t.Errorf("Expecting success, got %v", err)
	}

	_, err := builder.Build(c, test.NewEnv(t))
	if err == nil {
		t.Error("Expecting failure, got success")
	}
}

func TestReaper(t *testing.T) {
	limits := []config.Params_Quota{
		{
			Name:          "Q1",
			MaxAmount:     10,
			ValidDuration: 0,
		},
	}

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		MinDeduplicationDuration: 3600 * time.Second,
		Quotas: limits,
	}

	hndlr, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Errorf("Unable to create handler: %v", err)
	}

	h := hndlr.(*handler)

	now := time.Now()
	h.common.getTime = func() time.Time {
		return now
	}

	qa := adapter.QuotaRequestArgs{
		QuotaAmount: 10,
	}

	instance := quota.Instance{
		Name: "Q1",
	}

	qa.DeduplicationID = "0"
	qr, _ := h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", qr.Amount)
	}

	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", qr.Amount)
	}

	qa.DeduplicationID = "1"
	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", qr.Amount)
	}

	// move current dedup state into old dedup state
	h.common.reapDedup()

	qa.DeduplicationID = "2"
	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", qr.Amount)
	}

	// retire original dedup state
	h.common.reapDedup()

	qa.DeduplicationID = "0"
	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", qr.Amount)
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestReaperTicker(t *testing.T) {
	limits := []config.Params_Quota{
		{
			Name:          "Q1",
			MaxAmount:     10,
			ValidDuration: 0,
		},
	}

	testChan := make(chan time.Time)
	testTicker := &time.Ticker{C: testChan}

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder().(*builder)
	cfg := info.DefaultConfig.(*config.Params)
	cfg.Quotas = limits

	h, err := builder.buildWithDedup(cfg, test.NewEnv(t), testTicker)
	if err != nil {
		t.Errorf("Unable to create handler: %v", err)
	}

	qa := adapter.QuotaRequestArgs{
		QuotaAmount:     10,
		DeduplicationID: "0",
	}

	instance := quota.Instance{
		Name: "Q1",
	}

	qr, _ := h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", qr.Amount)
	}

	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 10 {
		t.Errorf("Alloc(): expecting 10, got %d", qr.Amount)
	}

	// Advance 3 ticks, ensuring clearing of the de-dup cache
	testChan <- time.Now()
	testChan <- time.Now()
	testChan <- time.Now()

	qa.DeduplicationID = "1"
	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", qr.Amount)
	}

	qa.DeduplicationID = "0"
	qr, _ = h.HandleQuota(context.Background(), &instance, qa)
	if qr.Amount != 0 {
		t.Errorf("Alloc(): expecting 0, got %d", qr.Amount)
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

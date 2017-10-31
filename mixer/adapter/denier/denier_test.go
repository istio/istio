// Copyright 2017 Istio Authors.
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

package denier

// NOTE: This test will eventually be auto-generated so that it automatically supports all CHECK and QUOTA
//       templates known to Mixer. For now, it's manually curated.

import (
	"context"
	"reflect"
	"testing"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/quota"
)

func TestBasic(t *testing.T) {
	info := GetInfo()

	if !contains(info.SupportedTemplates, checknothing.TemplateName) ||
		!contains(info.SupportedTemplates, listentry.TemplateName) ||
		!contains(info.SupportedTemplates, quota.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(info.DefaultConfig)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	checkNothingHandler := handler.(checknothing.Handler)
	result, err := checkNothingHandler.HandleCheckNothing(context.Background(), nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	want := newResult(defaultParam())

	if !reflect.DeepEqual(want, result) {
		t.Errorf("Got %v, expected %v", result, want)
	}

	listEntryHandler := handler.(listentry.Handler)
	result, err = listEntryHandler.HandleListEntry(context.Background(), nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if !reflect.DeepEqual(want, result) {
		t.Errorf("Got %v, expected %v", result, want)
	}

	quotaHandler := handler.(quota.Handler)
	qr, err := quotaHandler.HandleQuota(context.Background(), nil, adapter.QuotaArgs{QuotaAmount: 100})
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if qr.Amount != 0 {
		t.Errorf("Got %d quota, expecting 0", qr.Amount)
	}

	if qr.ValidDuration != 0 {
		t.Errorf("Got duration of %v, expecting at 0 seconds", qr.ValidDuration)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

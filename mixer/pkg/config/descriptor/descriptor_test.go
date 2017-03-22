// Copyright 2017 the Istio Authors.
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

package descriptor

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
)

type (
	getter func(Finder) (proto.Message, bool)

	cases []struct {
		name string
		cfg  *pb.GlobalConfig
		get  getter
		out  proto.Message
	}
)

var (
	logDesc = dpb.LogEntryDescriptor{
		Name:          "log",
		PayloadFormat: dpb.TEXT,
		LogTemplate:   "{{}}",
		Labels:        []*dpb.LabelDescriptor{},
	}

	getLog = func(k string) getter {
		return func(f Finder) (proto.Message, bool) {
			return f.GetLog(k)
		}
	}

	metricDesc = dpb.MetricDescriptor{
		Name:   "metric",
		Labels: []*dpb.LabelDescriptor{},
	}

	getMetric = func(k string) getter {
		return func(f Finder) (proto.Message, bool) {
			return f.GetMetric(k)
		}
	}

	monitoredResourceDesc = dpb.MonitoredResourceDescriptor{
		Name:   "mr",
		Labels: []*dpb.LabelDescriptor{},
	}

	getMR = func(k string) getter {
		return func(f Finder) (proto.Message, bool) {
			return f.GetMonitoredResource(k)
		}
	}

	principalDesc = dpb.PrincipalDescriptor{
		Name:   "principal",
		Labels: []*dpb.LabelDescriptor{},
	}

	getPrincipal = func(k string) getter {
		return func(f Finder) (proto.Message, bool) {
			return f.GetPrincipal(k)
		}
	}

	quotaDesc = dpb.QuotaDescriptor{
		Name:   "quota",
		Labels: []*dpb.LabelDescriptor{},
	}

	getQuota = func(k string) getter {
		return func(f Finder) (proto.Message, bool) {
			return f.GetQuota(k)
		}
	}
)

func TestGetLog(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Logs: []*dpb.LogEntryDescriptor{&logDesc}}, getLog("log"), &logDesc},
		{"missing", &pb.GlobalConfig{Logs: []*dpb.LogEntryDescriptor{&logDesc}}, getLog("foo"), nil},
		{"no logs", &pb.GlobalConfig{}, getLog("log"), nil},
	})
}

func TestGetMetric(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Metrics: []*dpb.MetricDescriptor{&metricDesc}}, getMetric("metric"), &metricDesc},
		{"missing", &pb.GlobalConfig{Metrics: []*dpb.MetricDescriptor{&metricDesc}}, getMetric("foo"), nil},
		{"no metrics", &pb.GlobalConfig{}, getMetric("metric"), nil},
	})
}

func TestGetMonitoredResource(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{MonitoredResources: []*dpb.MonitoredResourceDescriptor{&monitoredResourceDesc}}, getMR("mr"), &monitoredResourceDesc},
		{"missing", &pb.GlobalConfig{MonitoredResources: []*dpb.MonitoredResourceDescriptor{&monitoredResourceDesc}}, getMR("foo"), nil},
		{"no MRs", &pb.GlobalConfig{}, getMR("mr"), nil},
	})
}

func TestGetPrincipal(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Principals: []*dpb.PrincipalDescriptor{&principalDesc}}, getPrincipal("principal"), &principalDesc},
		{"missing", &pb.GlobalConfig{Principals: []*dpb.PrincipalDescriptor{&principalDesc}}, getPrincipal("foo"), nil},
		{"no principals", &pb.GlobalConfig{}, getPrincipal("principal"), nil},
	})
}

func TestGetQuota(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Quotas: []*dpb.QuotaDescriptor{&quotaDesc}}, getQuota("quota"), &quotaDesc},
		{"missing", &pb.GlobalConfig{Quotas: []*dpb.QuotaDescriptor{&quotaDesc}}, getQuota("foo"), nil},
		{"no quotas", &pb.GlobalConfig{}, getQuota("log"), nil},
	})
}

func execute(t *testing.T, tests cases) {
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			f := NewFinder(tt.cfg)
			d, found := tt.get(f)
			if !found && tt.out != nil {
				t.Fatalf("tt.fn() = _, false; expected descriptor %v", tt.out)
			}
			if tt.out != nil && !reflect.DeepEqual(d, tt.out) {
				t.Fatalf("tt.fn() = %v; expected descriptor %v", d, tt.out)
			}
		})
	}
}

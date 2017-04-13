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
	dpb "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

// Finder describes anything that can provide a view into the config's descriptors by name and type.
type Finder interface {
	expr.AttributeDescriptorFinder

	// GetLog retrieves the log descriptor named `name`
	GetLog(name string) *dpb.LogEntryDescriptor

	// GetMetric retrieves the metric descriptor named `name`
	GetMetric(name string) *dpb.MetricDescriptor

	// GetMonitoredResource retrieves the monitored resource descriptor named `name`
	GetMonitoredResource(name string) *dpb.MonitoredResourceDescriptor

	// GetPrincipal retrieves the security principal descriptor named `name`
	GetPrincipal(name string) *dpb.PrincipalDescriptor

	// GetQuota retrieves the quota descriptor named `name`
	GetQuota(name string) *dpb.QuotaDescriptor
}

type finder struct {
	logs               map[string]*dpb.LogEntryDescriptor
	metrics            map[string]*dpb.MetricDescriptor
	monitoredResources map[string]*dpb.MonitoredResourceDescriptor
	principals         map[string]*dpb.PrincipalDescriptor
	quotas             map[string]*dpb.QuotaDescriptor
	attributes         map[string]*dpb.AttributeDescriptor
}

// NewFinder constructs a new Finder for the provided global config.
func NewFinder(cfg *pb.GlobalConfig) Finder {
	logs := make(map[string]*dpb.LogEntryDescriptor)
	for _, desc := range cfg.Logs {
		logs[desc.Name] = desc
	}

	metrics := make(map[string]*dpb.MetricDescriptor)
	for _, desc := range cfg.Metrics {
		metrics[desc.Name] = desc
	}

	monitoredResources := make(map[string]*dpb.MonitoredResourceDescriptor)
	for _, desc := range cfg.MonitoredResources {
		monitoredResources[desc.Name] = desc
	}

	principals := make(map[string]*dpb.PrincipalDescriptor)
	for _, desc := range cfg.Principals {
		principals[desc.Name] = desc
	}

	quotas := make(map[string]*dpb.QuotaDescriptor)
	for _, desc := range cfg.Quotas {
		quotas[desc.Name] = desc
	}

	attributes := make(map[string]*dpb.AttributeDescriptor)
	for _, manifest := range cfg.Manifests {
		for _, desc := range manifest.Attributes {
			attributes[desc.Name] = desc
		}
	}

	return &finder{
		logs:               logs,
		metrics:            metrics,
		monitoredResources: monitoredResources,
		principals:         principals,
		quotas:             quotas,
		attributes:         attributes,
	}
}

func (d *finder) GetLog(name string) *dpb.LogEntryDescriptor {
	return d.logs[name]
}

func (d *finder) GetMetric(name string) *dpb.MetricDescriptor {
	return d.metrics[name]
}

func (d *finder) GetMonitoredResource(name string) *dpb.MonitoredResourceDescriptor {
	return d.monitoredResources[name]
}

func (d *finder) GetPrincipal(name string) *dpb.PrincipalDescriptor {
	return d.principals[name]
}

func (d *finder) GetQuota(name string) *dpb.QuotaDescriptor {
	return d.quotas[name]
}

func (d *finder) GetAttribute(name string) *dpb.AttributeDescriptor {
	return d.attributes[name]
}

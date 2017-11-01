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

package test

import (
	dpb "istio.io/api/mixer/v1/config/descriptor"
	cfgpb "istio.io/istio/mixer/pkg/config/proto"
)

// DescriptorFinder implements the descriptor.Finder interface more simply than the real object
// (namely in that it doesn't require synthesizing a config, and will panic when used incorrectly).
type DescriptorFinder struct {
	m map[string]interface{}
}

// NewDescriptorFinder returns a DescriptorFinder that will return values from desc when queried.
func NewDescriptorFinder(desc map[string]interface{}) *DescriptorFinder {
	return &DescriptorFinder{desc}
}

// GetLog returns the LogEntryDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetLog(name string) *dpb.LogEntryDescriptor {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*dpb.LogEntryDescriptor)
}

// GetMetric returns the MetricDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetMetric(name string) *dpb.MetricDescriptor {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*dpb.MetricDescriptor)
}

// GetMonitoredResource returns the MonitoredResourceDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetMonitoredResource(name string) *dpb.MonitoredResourceDescriptor {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*dpb.MonitoredResourceDescriptor)
}

// GetPrincipal returns the PrincipalDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetPrincipal(name string) *dpb.PrincipalDescriptor {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*dpb.PrincipalDescriptor)
}

// GetQuota returns the QuotaDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetQuota(name string) *dpb.QuotaDescriptor {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*dpb.QuotaDescriptor)
}

// GetAttribute returns the AttributeDescriptor named 'name' or nil if it does not exist in the map.
func (d *DescriptorFinder) GetAttribute(name string) *cfgpb.AttributeManifest_AttributeInfo {
	v, f := d.m[name]
	if !f {
		return nil
	}
	return v.(*cfgpb.AttributeManifest_AttributeInfo)
}

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

package adapter

import (
	"istio.io/mixer/adapter/denyChecker"
	"istio.io/mixer/adapter/genericListChecker"
	"istio.io/mixer/adapter/ipListChecker"
	"istio.io/mixer/adapter/kubernetes"
	"istio.io/mixer/adapter/memQuota"
	"istio.io/mixer/adapter/noop"
	"istio.io/mixer/adapter/prometheus"
	"istio.io/mixer/adapter/redisquota"
	"istio.io/mixer/adapter/serviceControl"
	"istio.io/mixer/adapter/statsd"
	"istio.io/mixer/adapter/stdioLogger"
	"istio.io/mixer/pkg/adapter"
)

// Inventory returns the inventory of all available adapters.
func Inventory() []adapter.RegisterFn {
	return []adapter.RegisterFn{
		denyChecker.Register,
		genericListChecker.Register,
		ipListChecker.Register,
		memQuota.Register,
		prometheus.Register,
		redisquota.Register,
		serviceControl.Register,
		statsd.Register,
		stdioLogger.Register,
		kubernetes.Register,
		noop.Register,
	}
}

// InventoryMap converts adapter inventory to a builder map.
func InventoryMap(inv []adapter.InfoFn) map[string]*adapter.BuilderInfo {
	m := make(map[string]*adapter.BuilderInfo, len(inv))
	for _, ai := range inv {
		bi := ai()
		m[bi.Name] = &bi
	}
	return m
}

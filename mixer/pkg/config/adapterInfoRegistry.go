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

package config

import (
	"fmt"
	"strings"

	"github.com/golang/glog"

	"istio.io/mixer/pkg/adapter"
)

type adapterInfoRegistry struct {
	adapterInfosByName map[string]*adapter.BuilderInfo
}

type handlerBuilderValidator func(hndlrBuilder adapter.HandlerBuilder, t string) (bool, string)

// newRegistry2 returns a new adapterInfoRegistry.
func newRegistry2(infos []adapter.InfoFn, hndlrBldrValidator handlerBuilderValidator) *adapterInfoRegistry {
	r := &adapterInfoRegistry{make(map[string]*adapter.BuilderInfo)}
	for idx, info := range infos {
		glog.V(3).Infof("registering [%d] %#v", idx, info)
		adptInfo := info()
		if a, ok := r.adapterInfosByName[adptInfo.Name]; ok {
			// panic only if 2 different adapter.BuilderInfo objects are trying to identify by the
			// same Name.
			msg := fmt.Errorf("duplicate registration for '%s' : old = %v new = %v", a.Name, adptInfo, a)
			glog.Error(msg)
			panic(msg)
		} else {
			if adptInfo.NewBuilder == nil {
				// panic if adapter has not provided the NewBuilder func.
				msg := fmt.Errorf("adapter info %v from adapter %s has nil NewBuilder", adptInfo, adptInfo.Name)
				glog.Error(msg)
				panic(msg)
			}
			if adptInfo.DefaultConfig == nil {
				// panic if adapter has not provided the DefaultConfig func.
				msg := fmt.Errorf("adapter info %v from adapter %s has nil DefaultConfig", adptInfo, adptInfo.Name)
				glog.Error(msg)
				panic(msg)
			}
			if ok, errMsg := doesBuilderSupportsTemplates(adptInfo, hndlrBldrValidator); !ok {
				// panic if an Adapter's HandlerBuilder does not implement interfaces that it says it wants to support.
				msg := fmt.Errorf("handlerBuilder from adapter %s does not implement the required interfaces"+
					" for the templates it supports: %s", adptInfo.Name, errMsg)
				glog.Error(msg)
				panic(msg)
			}

			r.adapterInfosByName[adptInfo.Name] = &adptInfo
		}
	}
	return r
}

// AdapterInfoMap returns the known adapter.Infos, indexed by their names.
func AdapterInfoMap(handlerRegFns []adapter.InfoFn,
	hndlrBldrValidator handlerBuilderValidator) map[string]*adapter.BuilderInfo {
	return newRegistry2(handlerRegFns, hndlrBldrValidator).adapterInfosByName
}

// FindAdapterInfo returns the adapter.BuilderInfo object with the given name.
func (r *adapterInfoRegistry) FindAdapterInfo(name string) (b *adapter.BuilderInfo, found bool) {
	bi, found := r.adapterInfosByName[name]
	if !found {
		return nil, false
	}
	return bi, true
}

func doesBuilderSupportsTemplates(info adapter.BuilderInfo, hndlrBldrValidator handlerBuilderValidator) (bool, string) {
	handlerBuilder := info.NewBuilder()
	resultMsgs := make([]string, 0)
	for _, t := range info.SupportedTemplates {
		if ok, errMsg := hndlrBldrValidator(handlerBuilder, t); !ok {
			resultMsgs = append(resultMsgs, errMsg)
		}
	}
	if len(resultMsgs) != 0 {
		return false, strings.Join(resultMsgs, "\n")
	}
	return true, ""
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

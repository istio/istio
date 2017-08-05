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

type builderInfoRegistry struct {
	builderInfosByName map[string]*adapter.BuilderInfo
}

type handlerBuilderValidator func(hndlrBuilder adapter.HandlerBuilder, t string) (bool, string)

// newRegistry2 returns a new BuilderInfo registry.
func newRegistry2(getBuilderInfos []adapter.GetBuilderInfoFn, hndlrBldrValidator handlerBuilderValidator) *builderInfoRegistry {
	r := &builderInfoRegistry{make(map[string]*adapter.BuilderInfo)}
	for idx, getBuilderInfo := range getBuilderInfos {
		glog.V(3).Infof("Registering [%d] %#v", idx, getBuilderInfo)
		bldrInfo := getBuilderInfo()
		if a, ok := r.builderInfosByName[bldrInfo.Name]; ok {
			// panic only if 2 different builderInfo objects are trying to identify by the
			// same Name.
			msg := fmt.Errorf("duplicate registration for '%s' : old = %v new = %v", a.Name, bldrInfo, a)
			glog.Error(msg)
			panic(msg)
		} else {
			if bldrInfo.ValidateConfig == nil {
				// panic if adapter has not provided the ValidateConfig func.
				msg := fmt.Errorf("BuilderInfo %v from adapter %s does not contain value for ValidateConfig"+
					" function field.", bldrInfo, bldrInfo.Name)
				glog.Error(msg)
				panic(msg)
			}
			if bldrInfo.DefaultConfig == nil {
				// panic if adapter has not provided the DefaultConfig func.
				msg := fmt.Errorf("BuilderInfo %v from adapter %s does not contain value for DefaultConfig "+
					"field.", bldrInfo, bldrInfo.Name)
				glog.Error(msg)
				panic(msg)
			}
			if ok, errMsg := doesBuilderSupportsTemplates(bldrInfo, hndlrBldrValidator); !ok {
				// panic if an Adapter's HandlerBuilder does not implement interfaces that it says it wants to support.
				msg := fmt.Errorf("HandlerBuilder from adapter %s does not implement the required interfaces"+
					" for the templates it supports: %s", bldrInfo.Name, errMsg)
				glog.Error(msg)
				panic(msg)
			}

			r.builderInfosByName[bldrInfo.Name] = &bldrInfo
		}
	}
	return r
}

// BuilderInfoMap returns the known BuilderInfos, indexed by their names.
func BuilderInfoMap(handlerRegFns []adapter.GetBuilderInfoFn,
	hndlrBldrValidator handlerBuilderValidator) map[string]*adapter.BuilderInfo {
	return newRegistry2(handlerRegFns, hndlrBldrValidator).builderInfosByName
}

// FindBuilderInfo returns the BuilderInfo object with the given name.
func (r *builderInfoRegistry) FindBuilderInfo(name string) (b *adapter.BuilderInfo, found bool) {
	bi, found := r.builderInfosByName[name]
	if !found {
		return nil, false
	}
	return bi, true
}

func doesBuilderSupportsTemplates(info adapter.BuilderInfo, hndlrBldrValidator handlerBuilderValidator) (bool, string) {
	handlerBuilder := info.CreateHandlerBuilderFn()
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

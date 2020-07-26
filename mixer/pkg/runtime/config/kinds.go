// Copyright Istio Authors
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
	"github.com/gogo/protobuf/proto"

	"istio.io/api/mixer/adapter/model/v1beta1"
	configpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/log"
)

// KindMap generates a map from object kind to its proto message.
func KindMap(adapterInfo map[string]*adapter.Info, templateInfo map[string]*template.Info) map[string]proto.Message {
	kindMap := make(map[string]proto.Message)
	// typed instances
	for kind, info := range templateInfo {
		kindMap[kind] = info.CtrCfg
		log.Debugf("template kind: %s, %v", kind, info.CtrCfg)
	}
	// typed handlers
	for kind, info := range adapterInfo {
		kindMap[kind] = info.DefaultConfig
		log.Debugf("adapter kind: %s, %v", kind, info.DefaultConfig)
	}
	kindMap[constant.RulesKind] = &configpb.Rule{}
	log.Debugf("Rules config kind: %s", constant.RulesKind)
	kindMap[constant.AttributeManifestKind] = &configpb.AttributeManifest{}
	log.Debugf("Attribute manifest kind: %s", constant.AttributeManifestKind)
	kindMap[constant.AdapterKind] = &v1beta1.Info{}
	log.Debugf("Adapter info config kind: %s", constant.AdapterKind)
	kindMap[constant.TemplateKind] = &v1beta1.Template{}
	log.Debugf("Template config kind: %s", constant.TemplateKind)
	kindMap[constant.InstanceKind] = &configpb.Instance{}
	log.Debugf("Instance config kind: %s", constant.InstanceKind)
	kindMap[constant.HandlerKind] = &configpb.Handler{}
	log.Debugf("Handler config kind: %s", constant.HandlerKind)
	return kindMap
}

// CriticalKinds returns kinds which are critical for mixer's function and have to be ready for mixer config watch.
func CriticalKinds() []string {
	return []string{
		constant.RulesKind,
		constant.AttributeManifestKind,
		constant.AdapterKind,
		constant.TemplateKind,
		constant.InstanceKind,
		constant.HandlerKind,
	}
}

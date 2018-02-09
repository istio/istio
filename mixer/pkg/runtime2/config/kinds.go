// Copyright 2018 Istio Authors
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

	configpb "istio.io/api/mixer/v1/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// KindMap generates a map from object kind to its proto message.
func KindMap(adapterInfo map[string]*adapter.Info, templateInfo map[string]*template.Info) map[string]proto.Message {
	kindMap := make(map[string]proto.Message)
	// typed instances
	for kind, info := range templateInfo {
		kindMap[kind] = info.CtrCfg
		log.Infof("template Kind: %s, %v", kind, info.CtrCfg)
	}
	// typed handlers
	for kind, info := range adapterInfo {
		kindMap[kind] = info.DefaultConfig
		log.Infof("adapter Kind: %s, %v", kind, info.DefaultConfig)
	}
	kindMap[RulesKind] = &configpb.Rule{}
	log.Infof("template Kind: %s", RulesKind)
	kindMap[AttributeManifestKind] = &configpb.AttributeManifest{}
	log.Infof("template Kind: %s", AttributeManifestKind)

	return kindMap
}

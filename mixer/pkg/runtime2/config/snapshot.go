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

package config

import (
	"github.com/gogo/protobuf/proto"
	"istio.io/istio/mixer/pkg/adapter"
	configpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/template"
)

var emptySnapshot = &Snapshot{
	attributes: emptyFinder,
}

type HandlerConfiguration struct {
	info *adapter.Info

	// fully qualified name
	name string

	params proto.Message

	config *configpb.Handler
}

type InstanceConfiguration struct {
	template template.Info

	// fully qualified name
	name string

	params proto.Message
}

type RuleConfiguration struct {
	config    *configpb.Rule
	namespace string
	actions   []*ActionConfiguration
}

type ActionConfiguration struct {
	handler   *HandlerConfiguration
	instances []*InstanceConfiguration
}

type Snapshot struct {
	// Static information
	templateInfos map[string]template.Info
	adapterInfos  map[string]*adapter.Info

	// Config store based information
	attributes *attributeFinder

	handlers  map[string]*HandlerConfiguration
	instances map[string]*InstanceConfiguration
	rules     []*RuleConfiguration
}

func Empty() *Snapshot {
	return emptySnapshot
}

func (s *Snapshot) Attributes() expr.AttributeDescriptorFinder {
	return s.attributes
}

func (s *Snapshot) AdapterInfo(name string) (*adapter.Info, bool) {
	info, found := s.adapterInfos[name]
	return info, found
}

func (s *Snapshot) TemplateInfo(name string) (template.Info, bool) {
	info, found := s.templateInfos[name]
	return info, found
}

func (s *Snapshot) Rules() []*RuleConfiguration {
	return s.rules
}

func (r *RuleConfiguration) Match() string {
	return r.config.Match
}

func (r *RuleConfiguration) Actions() []*ActionConfiguration {
	return r.actions
}

func (r *RuleConfiguration) Namespace() string {
	return r.namespace
}

func (a *ActionConfiguration) Handler() *HandlerConfiguration {
	return a.handler
}

func (a *ActionConfiguration) Instances() []*InstanceConfiguration {
	return a.instances
}

func (i *InstanceConfiguration) Template() template.Info {
	return i.template
}

func (i *InstanceConfiguration) Params() proto.Message {
	return i.params
}

func (h *HandlerConfiguration) Name() string {
	return h.name
}

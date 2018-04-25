//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package types

import (
	_ "istio.io/api/policy/v1beta1"
	_ "istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/kube/schema"
)

var Rule = &schema.Type{
	"rule",
	"rules",
	"config.istio.io",
	"v1beta1",
	"Rule",
	"RuleList",
	"istio.policy.v1beta1.Rule",
}

var ServiceConfig = &schema.Type{
	"producerservice",
	"producerservices",
	"config.istio.io",
	"dev",
	"ProducerService",
	"ProducerServiceList",
	"istio.service.dev.ProducerService",
}

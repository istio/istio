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

package iptables

import "istio.io/istio/cni/pkg/config"

type NetlinkDependencies interface {
	AddInpodMarkIPRule(cfg *config.AmbientConfig) error
	DelInpodMarkIPRule(cfg *config.AmbientConfig) error
	AddLoopbackRoutes(cfg *config.AmbientConfig) error
	DelLoopbackRoutes(cfg *config.AmbientConfig) error
}

func RealNlDeps() NetlinkDependencies {
	return &realDeps{}
}

type realDeps struct{}

func (r *realDeps) AddInpodMarkIPRule(cfg *config.AmbientConfig) error {
	return AddInpodMarkIPRule(cfg)
}

func (r *realDeps) DelInpodMarkIPRule(cfg *config.AmbientConfig) error {
	return DelInpodMarkIPRule(cfg)
}

func (r *realDeps) AddLoopbackRoutes(cfg *config.AmbientConfig) error {
	return AddLoopbackRoutes(cfg)
}

func (r *realDeps) DelLoopbackRoutes(cfg *config.AmbientConfig) error {
	return DelLoopbackRoutes(cfg)
}

type emptyDeps struct{}

func EmptyNlDeps() NetlinkDependencies {
	return &emptyDeps{}
}

func (r *emptyDeps) AddInpodMarkIPRule(cfg *config.AmbientConfig) error {
	return nil
}

func (r *emptyDeps) DelInpodMarkIPRule(cfg *config.AmbientConfig) error {
	return nil
}

func (r *emptyDeps) AddLoopbackRoutes(cfg *config.AmbientConfig) error {
	return nil
}

func (r *emptyDeps) DelLoopbackRoutes(cfg *config.AmbientConfig) error {
	return nil
}

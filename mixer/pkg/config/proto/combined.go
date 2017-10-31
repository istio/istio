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

package istio_mixer_v1_config

// Combined config is given to aspect managers.
type Combined struct {
	Builder   *Adapter
	Aspect    *Aspect
	Instances []*Instance
}

func (c *Combined) String() (ret string) {
	if c.Builder != nil {
		ret += "builder: " + c.Builder.String() + " "
	}
	if c.Aspect != nil {
		ret += "aspect: " + c.Aspect.String()
	}
	return
}

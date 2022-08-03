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

package param

// Params for a Template.
type Params map[string]any

// NewParams returns a new Params instance.
func NewParams() Params {
	return make(Params)
}

func (p Params) Get(k string) any {
	return p[k]
}

func (p Params) GetWellKnown(k WellKnown) any {
	return p[k.String()]
}

func (p Params) Set(k string, v any) Params {
	p[k] = v
	return p
}

func (p Params) SetAll(other Params) Params {
	for k, v := range other {
		p[k] = v
	}
	return p
}

func (p Params) SetAllNoOverwrite(other Params) Params {
	for k, v := range other {
		if !p.Contains(k) {
			p[k] = v
		}
	}
	return p
}

func (p Params) SetWellKnown(k WellKnown, v any) Params {
	p[k.String()] = v
	return p
}

func (p Params) Contains(k string) bool {
	_, found := p[k]
	return found
}

func (p Params) ContainsWellKnown(k WellKnown) bool {
	return p.Contains(k.String())
}

func (p Params) Copy() Params {
	if p == nil {
		return NewParams()
	}

	out := make(map[string]any)
	for k, v := range p {
		out[k] = v
	}
	return out
}

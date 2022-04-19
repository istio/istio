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

package namespace

var (
	chck Static
	_    Instance = &chck
)

// Static is a namespace that may or may not exist. It is used for configuration purposes only
type Static string

func (s Static) Name() string {
	return string(s)
}

func (s Static) Prefix() string {
	return string(s)
}

func (s Static) Labels() (map[string]string, error) {
	panic("implement me")
}

func (s Static) SetLabel(key, value string) error {
	panic("implement me")
}

func (s Static) RemoveLabel(key string) error {
	panic("implement me")
}

func (s *Static) UnmarshalJSON(bytes []byte) error {
	*s = Static(bytes)
	return nil
}

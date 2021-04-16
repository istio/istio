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

package v1alpha1

import (
	"github.com/golang/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MDPConfig struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty,inline"`
	Spec              *MDPConfigSpec `json:"spec,omitempty"`
	Status            *MDPStatus
}

func (m *MDPConfig) Reset()         { *m = MDPConfig{} }
func (m *MDPConfig) String() string { return proto.CompactTextString(m) }
func (*MDPConfig) ProtoMessage()    {}

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

package tombstone_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/source/kube/apiserver/tombstone"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestRecoverySuccessful(t *testing.T) {
	g := NewGomegaWithT(t)
	expected := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mynode",
		},
	}
	obj := tombstone.RecoverResource(cache.DeletedFinalStateUnknown{
		Obj: expected,
	})
	g.Expect(obj).To(Equal(expected))
}

func TestUnkownTypeShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)
	obj := tombstone.RecoverResource(&struct{}{})
	g.Expect(obj).To(BeNil())
}

func TestUnkownTombstoneObjectShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)
	obj := tombstone.RecoverResource(cache.DeletedFinalStateUnknown{
		Obj: &struct{}{},
	})
	g.Expect(obj).To(BeNil())
}

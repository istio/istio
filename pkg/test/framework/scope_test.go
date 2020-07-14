//  Copyright Istio Authors
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

package framework

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework/resource"
)

func TestGet_Struct(t *testing.T) {
	res := &resource.FakeResource{
		IDValue: "my-fake-resource",
	}

	tests := map[string]struct {
		setup    func() *scope
		expError error
	}{
		"exists": {
			setup: func() *scope {
				scope := newScope("s", nil)
				scope.add(res, &resourceID{id: res.IDValue})
				return scope
			},
		},
		"parent": {
			setup: func() *scope {
				p := newScope("p", nil)
				p.add(res, &resourceID{id: res.IDValue})
				scope := newScope("s", p)
				return scope
			},
		},
		"missing": {
			setup:    func() *scope { return newScope("s", nil) },
			expError: fmt.Errorf("no framework.OtherInterface in context"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			scope := tt.setup()
			var got OtherInterface
			err := scope.get(&got)
			if tt.expError == nil {
				g.Expect(err).To(BeNil())
				g.Expect(got).To(Equal(res))
			} else {
				g.Expect(err).To(Equal(tt.expError))
			}
		})
	}
}

func TestGet_Slice(t *testing.T) {
	exp := []*resource.FakeResource{
		{
			IDValue:    "child-resource",
			OtherValue: "child",
		},
		{
			IDValue:    "parent-resource",
			OtherValue: "parent",
		},
	}

	g := NewGomegaWithT(t)
	parent := newScope("parent", nil)
	parent.add(exp[1], &resourceID{id: exp[1].IDValue})
	child := newScope("child", parent)
	child.add(exp[0], &resourceID{id: exp[0].IDValue})
	var got []OtherInterface
	err := child.get(&got)
	g.Expect(err).To(BeNil())
	g.Expect(got).To(HaveLen(len(exp)))
	for i, res := range exp {
		g.Expect(got[i]).To(Equal(res))
	}
}

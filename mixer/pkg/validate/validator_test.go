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

package validate

import (
	"errors"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/config/store"
)

func backendEvent(t store.ChangeType, kind, name string, spec map[string]interface{}) *store.BackendEvent {
	namespace := "ns"
	return &store.BackendEvent{
		Type: t,
		Key:  store.Key{Kind: kind, Namespace: namespace, Name: name},
		Value: &store.BackEndResource{
			Metadata: store.ResourceMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: spec,
		},
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		title string
		ev    *store.BackendEvent
		want  error
	}{
		{
			"update",
			backendEvent(store.Update, "handler", "foo", map[string]interface{}{"compiledAdapter": "noop", "name": "default"}),
			nil,
		},
		{
			"update error (shallow)",
			backendEvent(store.Update, "handler", "foo", map[string]interface{}{"adapterr": "noop"}),
			errors.New(`unknown field "adapterr" in v1beta1.Handler`),
		},
		{
			"update error (referential)",
			backendEvent(store.Update, "handler", "foo", map[string]interface{}{"adapter": "prometheus"}),
			errors.New(`handler='foo.handler.ns'.adapter: adapter 'prometheus' not found`),
		},
		{
			"delete",
			backendEvent(store.Delete, "handler", "foo", nil),
			nil,
		},
		{
			"unknown kinds",
			backendEvent(store.Update, "unknown", "bar", map[string]interface{}{"foo": "bar"}),
			nil,
		},
	} {
		t.Run(c.title, func(tt *testing.T) {
			v := NewDefaultValidator(true)
			err := v.Validate(c.ev)
			if c.want == nil && err != nil {
				tt.Errorf("Got %v, Want nil", err)
			} else if c.want != nil && !strings.Contains(err.Error(), c.want.Error()) {
				tt.Errorf("Got %v, Want to contain %s", err, c.want.Error())
			}
		})
	}
}

func TestSupportsKind(t *testing.T) {
	v := NewDefaultValidator(true)

	if got := v.SupportsKind("instance"); !got {
		t.Errorf("SupportsKind => got false for instance")
	}
}

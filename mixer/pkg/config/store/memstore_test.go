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

package store

import (
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
)

func TestGetMemstoreWriter(t *testing.T) {
	u := "memstore://" + t.Name()
	mw := GetMemstoreWriter(u)
	if mw == nil {
		t.Fatal("Failed to get memstore writer")
	}

	if mw2 := GetMemstoreWriter(u); mw != mw2 {
		t.Errorf("Two invocation of GetMemstoreWriter needs to return the same object: %s", u)
	}
}

const cfg1 = `
kind: rule
apiVersion: testing.istio.io/v1alpha1
metadata:
  name: foo
  namespace: ns1
spec:
---
kind: rule
apiVersion: testing.istio.io/v1alpha1
metadata:
  name: bar
  namespace: ns1
spec:
---
`

const cfg2 = `
kind: rule
apiVersion: testing.istio.io/v1alpha1
metadata:
  name: bazz
  namespace: ns2
spec:
---
`

const errCfg = `
kind: 1
`

func TestSetupMemstore(t *testing.T) {
	for _, c := range []struct {
		title   string
		configs []string
		ok      bool
		count   int
	}{
		{
			"merge",
			[]string{cfg1, cfg2},
			true,
			3,
		},
		{
			"error",
			[]string{errCfg},
			false,
			0,
		},
		{
			"merge-with-error",
			[]string{cfg1, errCfg, cfg2},
			false,
			3,
		},
		{
			"duplicate",
			[]string{cfg1, cfg1},
			true,
			2,
		},
	} {
		t.Run(c.title, func(t *testing.T) {
			u := "memstore://" + t.Name()
			err := SetupMemstore(u, c.configs...)
			gotOK := err == nil
			if c.ok != gotOK {
				t.Fatalf("Failed the setup: got %v(error %v), want %v", gotOK, err, c.ok)
			}
			s, err := NewRegistry().NewStore(u)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err = s.Init(ctx, map[string]proto.Message{"rule": &types.Empty{}}); err != nil {
				t.Fatalf("Failed to init: %v", err)
			}
			if d := s.List(); len(d) != c.count {
				t.Errorf("Got %d (%+v), Want %d", len(d), d, c.count)
			}
		})
	}
}

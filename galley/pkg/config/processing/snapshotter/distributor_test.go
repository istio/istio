// Copyright 2019 Istio Authors
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

package snapshotter

import (
	"testing"

	sn "istio.io/istio/pkg/mcp/snapshot"
)

func TestDistributor_SetSnapshot(t *testing.T) {
	d := NewInMemoryDistributor()

	b := sn.NewInMemoryBuilder()
	s := b.Build()
	d.SetSnapshot("foo", s)
	if _, ok := d.snapshots["foo"]; !ok {
		t.Fatal("The snapshot should have been set")
	}
}

func TestDistributor_GetSnapshot(t *testing.T) {
	d := NewInMemoryDistributor()

	b := sn.NewInMemoryBuilder()
	s := b.Build()
	d.SetSnapshot("foo", s)

	sn := d.GetSnapshot("foo")
	if sn != s {
		t.Fatal("The snapshots should have been the same")
	}
}

func TestDistributor_GetSnapshot_Unknown(t *testing.T) {
	d := NewInMemoryDistributor()

	b := sn.NewInMemoryBuilder()
	s := b.Build()
	d.SetSnapshot("foo", s)

	sn := d.GetSnapshot("bar")
	if sn != nil {
		t.Fatal("The snapshots should have been nil")
	}
}

func TestDistributor_NumSnapshots(t *testing.T) {
	d := NewInMemoryDistributor()

	b := sn.NewInMemoryBuilder()
	s := b.Build()
	d.SetSnapshot("foo", s)

	if d.NumSnapshots() != 1 {
		t.Fatal("The snapshots should have been 1")
	}
}

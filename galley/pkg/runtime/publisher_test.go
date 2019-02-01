//  Copyright 2019 Istio Authors
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

package runtime

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/testing/resources"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/test/util/retry"
)

func TestPublisher_CollectionChanged_NotStarted(t *testing.T) {
	sn := processing.SnapshotterFromFn(func() snapshot.Snapshot {
		return nil
	})

	dist := NewInMemoryDistributor()

	str := publish.NewStrategy(time.Nanosecond, time.Nanosecond, time.Nanosecond)
	p := newPublisher("sn1", sn, dist, str)
	defer func() { _ = p.Close() }()

	triggered := false
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-done:
			return
		case <-p.channel():
			triggered = true
		}
	}()

	p.CollectionChanged(resources.EmptyInfo.Collection)

	// Wait long-enough
	time.Sleep(time.Second)

	if triggered {
		t.Fatalf("Listener event should not have triggered publishing")
	}
}

func TestPublisher_CollectionChanged_Started(t *testing.T) {
	sn := processing.SnapshotterFromFn(func() snapshot.Snapshot {
		return nil
	})

	dist := NewInMemoryDistributor()

	str := publish.NewStrategy(time.Nanosecond, time.Nanosecond, time.Nanosecond)
	p := newPublisher("sn1", sn, dist, str)
	defer func() { _ = p.Close() }()

	triggers := 0
	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-p.channel():
				triggers++
			}
		}
	}()

	p.start()

	// We expect at least 2 triggers: the initial one, followed ColllectionChanged.
	_, err := retry.Do(func() (interface{}, bool, error) {
		if triggers > 0 {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("publishing is not triggered")
	})
	if err != nil {
		t.Fatal(err)
	}

	p.CollectionChanged(resources.EmptyInfo.Collection)

	_, err = retry.Do(func() (interface{}, bool, error) {
		if triggers > 1 {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("publishing is not triggered")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublisher_Publish(t *testing.T) {
	sn := processing.SnapshotterFromFn(func() snapshot.Snapshot {
		return snapshot.NewInMemoryBuilder().Build()
	})

	dist := &fakeDistributor{}

	str := publish.NewStrategy(time.Nanosecond, time.Nanosecond, time.Nanosecond)
	p := newPublisher("sn1", sn, dist, str)
	defer func() { _ = p.Close() }()

	p.publish()

	if dist.name != "sn1" || dist.snapshot == nil {
		t.Fatalf("snapshot was not published")
	}
}

type fakeDistributor struct {
	name     string
	snapshot snapshot.Snapshot
}

var _ Distributor = &fakeDistributor{}

func (f *fakeDistributor) SetSnapshot(name string, snapshot snapshot.Snapshot) {
	f.name = name
	f.snapshot = snapshot
}

func (f *fakeDistributor) ClearSnapshot(name string) {
	f.name = ""
	f.snapshot = nil
}

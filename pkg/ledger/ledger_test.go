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

package ledger

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spaolacci/murmur3"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/util/assert"
)

func TestLongKeys(t *testing.T) {
	longKey := "virtual-service/frontend/default"
	l := smtLedger{tree: newSMT(hasher, nil, time.Minute)}
	_, err := l.Put(longKey+"1", "1")
	assert.NoError(t, err)
	_, err = l.Put(longKey+"2", "2")
	assert.NoError(t, err)
	res, err := l.Get(longKey + "1")
	assert.NoError(t, err)
	assert.Equal(t, res, "1")
	res, err = l.Get(longKey + "2")
	assert.NoError(t, err)
	assert.Equal(t, res, "2")
	res, err = l.Get(longKey)
	assert.NoError(t, err)
	assert.Equal(t, res, "")
}

func TestGetAndPrevious(t *testing.T) {
	l := smtLedger{tree: newSMT(hasher, nil, time.Minute)}
	resultHashes := map[string]bool{}
	l.Put("foo", "bar")
	firstHash := l.RootHash()

	resultHashes[l.RootHash()] = true
	l.Put("foo", "baz")
	resultHashes[l.RootHash()] = true
	l.Put("second", "value")
	resultHashes[l.RootHash()] = true
	getResult, err := l.Get("foo")
	assert.NoError(t, err)
	assert.Equal(t, getResult, "baz")
	getResult, err = l.Get("second")
	assert.NoError(t, err)
	assert.Equal(t, getResult, "value")
	getResult, err = l.GetPreviousValue(firstHash, "foo")
	assert.NoError(t, err)
	assert.Equal(t, getResult, "bar")
	if len(resultHashes) != 3 {
		t.Fatal("Encountered has collision")
	}
}

func TestOrderAgnosticism(t *testing.T) {
	l := smtLedger{tree: newSMT(MyHasher, nil, time.Minute)}
	_, err := l.Put("foo", "bar")
	assert.NoError(t, err)
	firstHash, err := l.Put("second", "value")
	assert.NoError(t, err)
	secondHash, err := l.Put("foo", "baz")
	assert.NoError(t, err)
	assert.Equal(t, firstHash != secondHash, true)
	lastHash, err := l.Put("foo", "bar")
	assert.NoError(t, err)
	assert.Equal(t, firstHash, lastHash)
}

func MyHasher(data ...[]byte) (result []byte) {
	hasher := murmur3.New64()
	for i := 0; i < len(data); i++ {
		hasher.Write(data[i])
	}
	result = hasher.Sum(nil)
	hasher.Reset()
	return
}

func TestCollision(t *testing.T) {
	hit := false
	HashCollider := func(data ...[]byte) []byte {
		if hit {
			return []byte{
				0xde,
				0xad,
				0xbe,
				0xef,
				0xde,
				0xad,
				0xbe,
				0xef,
			}
		}
		return MyHasher(data...)
	}
	l := smtLedger{tree: newSMT(HashCollider, nil, time.Minute)}
	hit = true
	_, err := l.Put("foo", "bar")
	assert.NoError(t, err)
	_, err = l.Put("fhgwgads", "shouldcollide")
	assert.NoError(t, err)
	value, err := l.Get("foo")
	assert.NoError(t, err)
	assert.Equal(t, value, "bar")
}

func HashCollider(data ...[]byte) []byte {
	return MyHasher(data...)
}

// nolint: gosec
// test only code
func BenchmarkScale(b *testing.B) {
	const configSize = 100
	b.ReportAllocs()
	b.SetBytes(8)
	l := &smtLedger{tree: newSMT(HashCollider, nil, time.Minute)}
	var eg errgroup.Group
	ids := make([]string, 0, configSize)
	for i := 0; i < configSize; i++ {
		ids = append(ids, addConfig(l, b))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		eg.Go(func() error {
			_, err := l.Put(ids[rand.Int()%configSize], strconv.Itoa(rand.Int()))
			_ = l.RootHash()
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		b.Fatalf("An error occurred putting new data on the ledger: %v", err)
	}
	b.StopTimer()
}

// nolint: gosec
// test only code
func addConfig(ledger Ledger, b *testing.B) string {
	objectID := strings.Replace(uuid.New().String(), "-", "", -1)
	_, err := ledger.Put(objectID, fmt.Sprintf("%d", rand.Int()))
	assert.NoError(b, err)
	return objectID
}

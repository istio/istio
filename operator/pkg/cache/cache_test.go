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

package cache

import (
	"reflect"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/operator/pkg/object"
)

func TestFlushObjectCaches(t *testing.T) {
	tests := []struct {
		desc     string
		wantSize int
	}{
		{
			desc:     "flush-cache",
			wantSize: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			unstObjs := make(map[string]any)
			tUnstructured := &unstructured.Unstructured{Object: unstObjs}
			testCache := make(map[string]*object.K8sObject)
			testCache["foo"] = object.NewK8sObject(tUnstructured, nil, nil)
			objectCaches["foo"] = &ObjectCache{
				Cache: testCache,
				Mu:    &sync.RWMutex{},
			}
			if len(objectCaches) != 1 {
				t.Errorf("%s: Expected len 1, got len 0.", tt.desc)
			}
			FlushObjectCaches()
			if gotLen := len(objectCaches); gotLen != tt.wantSize {
				t.Errorf("%s: Expected size %v after flush, got size %v", tt.desc, tt.wantSize, gotLen)
			}
		})
	}
}

func TestGetCache(t *testing.T) {
	tests := []struct {
		desc string
		key  string
		in   map[string]*ObjectCache
		want ObjectCache
	}{
		{
			desc: "value-exists",
			key:  "foo-key",
			in: map[string]*ObjectCache{
				"foo-key": {
					Cache: make(map[string]*object.K8sObject),
					Mu:    nil,
				},
			},
			want: ObjectCache{
				Cache: make(map[string]*object.K8sObject),
				Mu:    nil,
			},
		},
		{
			desc: "key-does-not-exist",
			key:  "foo-key",
			in:   make(map[string]*ObjectCache),
			want: ObjectCache{
				Cache: make(map[string]*object.K8sObject),
				Mu:    &sync.RWMutex{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			defer FlushObjectCaches()
			for key, value := range tt.in {
				objectCaches[key] = value
			}
			if gotCache := GetCache(tt.key); !reflect.DeepEqual(*gotCache, tt.want) {
				t.Errorf("%s: expected cache %v, got cache %v\n", tt.desc, tt.want, *gotCache)
			}
		})
	}
}

func TestRemoveObject(t *testing.T) {
	tests := []struct {
		desc string
		in   map[string]*ObjectCache
		// key for map of caces
		objCacheRemovalKey string
		// key for map of K8sObjects
		removalKey string
		// cache in position objectCaches[objCacheRemovalKey]
		expectedCache ObjectCache
	}{
		{
			desc: "remove-cache",
			in: map[string]*ObjectCache{
				"cache-foo-key": {
					Cache: map[string]*object.K8sObject{
						"obj-foo-key": object.NewK8sObject(&unstructured.Unstructured{
							Object: make(map[string]any),
						}, nil, nil),
						"dont-touch-me-key": object.NewK8sObject(&unstructured.Unstructured{
							Object: make(map[string]any),
						}, nil, nil),
					},
					Mu: &sync.RWMutex{},
				},
			},
			objCacheRemovalKey: "cache-foo-key",
			removalKey:         "obj-foo-key",
			expectedCache: ObjectCache{
				Cache: map[string]*object.K8sObject{
					"dont-touch-me-key": object.NewK8sObject(&unstructured.Unstructured{
						Object: make(map[string]any),
					}, nil, nil),
				},
				Mu: &sync.RWMutex{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			for key, value := range tt.in {
				objectCaches[key] = value
			}
			defer FlushObjectCaches()
			RemoveObject(tt.objCacheRemovalKey, tt.removalKey)
			if got := objectCaches[tt.objCacheRemovalKey]; !reflect.DeepEqual(*got, tt.expectedCache) {
				t.Errorf("%s: expected object cache %v, got %v\n", tt.desc, tt.expectedCache, got)
			}
		})
	}
}

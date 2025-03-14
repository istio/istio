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

package krt

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	acmetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

func getTypedKey[O any](a O) Key[O] {
	return Key[O](GetKey(a))
}

// Needed to separate this out since Go generics doesn't allow for
// recursion and we need to support collections of ObjectWithCluster
func getKeyInner[O any](a O) string {
	as, ok := any(a).(string)
	if ok {
		return as
	}
	ao, ok := any(a).(controllers.Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return k
	}
	ac, ok := any(a).(config.Config)
	if ok {
		return keyFunc(ac.Name, ac.Namespace)
	}
	acp, ok := any(a).(*config.Config)
	if ok {
		return keyFunc(acp.Name, acp.Namespace)
	}
	arn, ok := any(a).(ResourceNamer)
	if ok {
		return arn.ResourceName()
	}
	auid, ok := any(a).(uidable)
	if ok {
		return strconv.FormatUint(uint64(auid.uid()), 10)
	}

	akclient, ok := any(a).(kube.Client)
	if ok {
		return string(akclient.ClusterID())
	}

	ack := GetApplyConfigKey(a)
	if ack != nil {
		return *ack
	}
	panic(fmt.Sprintf("Cannot get Key, got %T", a))
}

// GetKey returns the key for the provided object.
// If there is none, this will panic.
func GetKey[O any](a O) string {
	acluster, ok := any(a).(config.ObjectWithCluster[O])
	if ok {
		return getKeyInner(acluster.Object)
	}

	return getKeyInner(a)
}

// Named is a convenience struct. It is ideal to be embedded into a type that has a name and namespace,
// and will automatically implement the various interfaces to return the name, namespace, and a key based on these two.
type Named struct {
	Name, Namespace string
}

// NewNamed builds a Named object from a Kubernetes object type.
func NewNamed(o metav1.Object) Named {
	return Named{Name: o.GetName(), Namespace: o.GetNamespace()}
}

func (n Named) ResourceName() string {
	return n.Namespace + "/" + n.Name
}

func (n Named) GetName() string {
	return n.Name
}

func (n Named) GetNamespace() string {
	return n.Namespace
}

// GetApplyConfigKey returns the key for the ApplyConfig.
// If there is none, this will return nil.
func GetApplyConfigKey[O any](a O) *string {
	// Reflection is expensive; short circuit here
	if !strings.HasSuffix(ptr.TypeName[O](), "ApplyConfiguration") {
		return nil
	}
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	specField := val.FieldByName("ObjectMetaApplyConfiguration")
	if !specField.IsValid() {
		return nil
	}
	meta := specField.Interface().(*acmetav1.ObjectMetaApplyConfiguration)
	if meta.Namespace != nil && len(*meta.Namespace) > 0 {
		return ptr.Of(*meta.Namespace + "/" + *meta.Name)
	}
	return meta.Name
}

// keyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func keyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

func waitForCacheSync(name string, stop <-chan struct{}, collections ...<-chan struct{}) (r bool) {
	t := time.NewTicker(time.Second * 5)
	defer t.Stop()
	t0 := time.Now()
	defer func() {
		if r {
			log.WithLabels("name", name, "time", time.Since(t0)).Debugf("sync complete")
		} else {
			log.WithLabels("name", name, "time", time.Since(t0)).Errorf("sync failed")
		}
	}()
	for _, col := range collections {
		for {
			select {
			case <-t.C:
				log.WithLabels("name", name, "time", time.Since(t0)).Debugf("waiting for sync...")
				continue
			case <-stop:
				return false
			case <-col:
			}
			break
		}
	}
	return true
}

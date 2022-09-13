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

package kube

import (
	"context"
	"reflect"

	gogoproto "github.com/gogo/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
)

type convertFn func(obj any) metav1.Common

type untypedMirror struct {
	createClient reflect.Value
	queue        queue.Instance
	convertRes   convertFn
}

func mirrorTo(q queue.Instance, createClient any, convert convertFn) *untypedMirror {
	// TODO go 1.18 generics may help avoid reflection
	untyped := reflect.ValueOf(createClient)
	if untyped.Type().Kind() != reflect.Func {
		panic("non-func passed to mirrorTo")
	}
	return &untypedMirror{
		createClient: untyped,
		queue:        q,
		convertRes:   convert,
	}
}

func (c *untypedMirror) OnAdd(obj any) {
	meta := obj.(metav1.Object)
	res := c.convertRes(obj)
	if res == nil {
		log.Warnf("failed to mirror resource %v/%v", meta.GetName(), meta.GetName())
		return
	}
	log.Debugf("Mirroring ADD %s/%s", meta.GetName(), meta.GetName())
	c.queue.Push(func() error {
		return c.Create(meta.GetNamespace(), res)
	})
}

func (c *untypedMirror) OnUpdate(_, obj any) {
	meta := obj.(metav1.Object)
	res := c.convertRes(obj)
	if res == nil {
		log.Warnf("failed to mirror resource %v/%v", meta.GetName(), meta.GetName())
		return
	}
	res.SetResourceVersion("")
	log.Debugf("Mirroring UPDATE %s/%s", meta.GetName(), meta.GetName())
	c.queue.Push(func() error {
		return c.Update(meta.GetNamespace(), res)
	})
}

func (c *untypedMirror) OnDelete(obj any) {
	meta := obj.(metav1.Object)
	log.Debugf("Mirroring DELETE %s/%s", meta.GetName(), meta.GetName())
	c.queue.Push(func() error {
		return c.Delete(meta.GetNamespace(), meta.GetName())
	})
}

func (c *untypedMirror) do(ns string, method string, args ...any) []reflect.Value {
	return c.createClient.Call(argValues(ns))[0].MethodByName(method).Call(argValues(args...))
}

func (c *untypedMirror) Create(ns string, obj any) error {
	ret := c.do(ns, "Create", context.TODO(), obj, metav1.CreateOptions{})
	err, _ := ret[1].Interface().(error)
	return err
}

func (c *untypedMirror) Update(ns string, obj any) error {
	ret := c.do(ns, "Update", context.TODO(), obj, metav1.UpdateOptions{})
	err, _ := ret[1].Interface().(error)
	return err
}

func (c *untypedMirror) Delete(ns string, name string) error {
	ret := c.do(ns, "Delete", context.TODO(), name, metav1.DeleteOptions{})
	err, _ := ret[0].Interface().(error)
	return err
}

func argValues(args ...any) []reflect.Value {
	out := make([]reflect.Value, len(args))
	for i, arg := range args {
		out[i] = reflect.ValueOf(arg)
	}
	return out
}

func mirrorResource(q queue.Instance, from cache.SharedIndexInformer, toClientFunc any, convertFn convertFn) {
	from.AddEventHandler(mirrorTo(q, toClientFunc, convertFn))
}

func endpointSliceV1toV1beta1(obj any) metav1.Common {
	in, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return nil
	}
	marshaled, err := gogoproto.Marshal(in)
	if err != nil {
		return nil
	}
	out := &discoveryv1beta1.EndpointSlice{}
	if err := gogoproto.Unmarshal(marshaled, out); err != nil {
		return nil
	}
	for i, endpoint := range out.Endpoints {
		endpoint.Topology = in.Endpoints[i].DeprecatedTopology
		if in.Endpoints[i].Zone != nil {
			// not sure if this is 100% accurate
			endpoint.Topology[corev1.LabelTopologyRegion] = *in.Endpoints[i].Zone
			endpoint.Topology[corev1.LabelTopologyZone] = *in.Endpoints[i].Zone
		}
		out.Endpoints[i] = endpoint
	}
	return out
}

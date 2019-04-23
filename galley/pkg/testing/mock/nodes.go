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

package mock

import (
	"fmt"
	"sync"

	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var _ corev1.NodeInterface = &nodeImpl{}

type nodeImpl struct {
	mux     sync.Mutex
	nodes   map[string]*apicorev1.Node
	watches Watches
}

func newNodeInterface() corev1.NodeInterface {
	return &nodeImpl{
		nodes: make(map[string]*apicorev1.Node),
	}
}

func (n *nodeImpl) Create(obj *apicorev1.Node) (*apicorev1.Node, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	n.nodes[obj.Name] = obj

	n.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (n *nodeImpl) Update(obj *apicorev1.Node) (*apicorev1.Node, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	n.nodes[obj.Name] = obj

	n.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (n *nodeImpl) Delete(name string, options *metav1.DeleteOptions) error {
	n.mux.Lock()
	defer n.mux.Unlock()

	obj := n.nodes[name]
	if obj == nil {
		return fmt.Errorf("unable to delete node %s", name)
	}

	delete(n.nodes, name)

	n.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (n *nodeImpl) List(opts metav1.ListOptions) (*apicorev1.NodeList, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	out := &apicorev1.NodeList{}

	for _, v := range n.nodes {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (n *nodeImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	w := NewWatch()
	n.watches = append(n.watches, w)

	// Send add events for all current resources.
	for _, node := range n.nodes {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: node,
		})
	}

	return w, nil
}

func (n *nodeImpl) UpdateStatus(*apicorev1.Node) (*apicorev1.Node, error) {
	panic("not implemented")
}

func (n *nodeImpl) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (n *nodeImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Node, error) {
	panic("not implemented")
}

func (n *nodeImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Node, err error) {
	panic("not implemented")
}

func (n *nodeImpl) PatchStatus(nodeName string, data []byte) (*apicorev1.Node, error) {
	panic("not implemented")
}

//  Copyright 2018 Istio Authors
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

package mock

import (
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

// Interface is the mock of apiext.CustomResourceDefinitionInterface.
type Interface struct {
	responses    chan response
	callers      int32
	shutdown     chan struct{}

	log     string
	logLock sync.Mutex
}

type response struct {
	create1           *apiext.CustomResourceDefinition
	create2           error
	update1           *apiext.CustomResourceDefinition
	update2           error
	updateStatus1     *apiext.CustomResourceDefinition
	updateStatus2     error
	delete1           error
	deleteCollection1 error
	get1              *apiext.CustomResourceDefinition
	get2              error
	list1             *apiext.CustomResourceDefinitionList
	list2             error
	watch1            watch.Interface
	watch2            error
	patch1            *apiext.CustomResourceDefinition
	patch2            error
	panic             string
}

var _ v1beta1.CustomResourceDefinitionInterface = &Interface{}

// NewInterface returns a new mock Interface.
func NewInterface() *Interface {
	return &Interface{
		responses: make(chan response, 1024),
		shutdown:  make(chan struct{}),
	}
}

// Create is implementation of CustomResourceDefinitionInterface.Create.
func (m *Interface) Create(c *apiext.CustomResourceDefinition) (*apiext.CustomResourceDefinition, error) {
	m.appendToLog("CREATE: %s\n", c.Name)
	r := m.dequeueResponse()
	return r.create1, r.create2
}

// Update is implementation of CustomResourceDefinitionInterface.Update.
func (m *Interface) Update(c *apiext.CustomResourceDefinition) (*apiext.CustomResourceDefinition, error) {

	m.appendToLog("UPDATE: %s\n", c.Name)
	r := m.dequeueResponse()
	return r.update1, r.update2
}

// UpdateStatus is implementation of CustomResourceDefinitionInterface.UpdateStatus.
func (m *Interface) UpdateStatus(c *apiext.CustomResourceDefinition) (*apiext.CustomResourceDefinition, error) {
	m.appendToLog("UPSTATESTATUS: %s\n", c.Name)
	r := m.dequeueResponse()
	return r.updateStatus1, r.updateStatus2
}

// Delete is implementation of CustomResourceDefinitionInterface.Deelte.
func (m *Interface) Delete(name string, options *v1.DeleteOptions) error {
	m.appendToLog("DELETE: %s\n", name)
	r := m.dequeueResponse()
	return r.delete1
}

// DeleteCollection is implementation of CustomResourceDefinitionInterface.DeleteCollection.
func (m *Interface) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	m.appendToLog("DELETECOLLECTION\n")
	r := m.dequeueResponse()
	return r.deleteCollection1
}

// Get is implementation of CustomResourceDefinitionInterface.Get.
func (m *Interface) Get(name string, options v1.GetOptions) (*apiext.CustomResourceDefinition, error) {
	m.appendToLog("GET: %s\n", name)
	r := m.dequeueResponse()
	return r.get1, r.get2
}

// List is implementation of CustomResourceDefinitionInterface.List.
func (m *Interface) List(opts v1.ListOptions) (*apiext.CustomResourceDefinitionList, error) {
	m.appendToLog("LIST\n")
	r := m.dequeueResponse()
	return r.list1, r.list2
}

// Watch is implementation of CustomResourceDefinitionInterface.Watch.
func (m *Interface) Watch(opts v1.ListOptions) (watch.Interface, error) {
	m.appendToLog("WATCH\n")
	r := m.dequeueResponse()
	return r.watch1, r.watch2
}

// Patch is implementation of CustomResourceDefinitionInterface.Patch.
func (m *Interface) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apiext.CustomResourceDefinition, err error) {
	m.appendToLog("PATCH %s\n", name)
	r := m.dequeueResponse()
	return r.patch1, r.patch2
}

func (m *Interface) dequeueResponse() (r response) {
	atomic.AddInt32(&m.callers, 1)

	sh := false
	select {
	case <-m.shutdown:
		sh = true
	case r = <-m.responses:
	}

	atomic.AddInt32(&m.callers, -1)

	if sh {
		m.appendToLog("!! Straggler found !!%v\n", string(debug.Stack()))
		panic(m.String())
	}

	// Handle panic at this level. It applies to all external methods.
	if r.panic != "" {
		panic(r.panic)
	}

	return
}

func (m *Interface) enqueueResponse(r response) {
	m.responses <- r
}

// AddListResponse enqueues a new List response.
func (m *Interface) AddListResponse(list *apiext.CustomResourceDefinitionList, err error) {
	m.enqueueResponse(response{
		list1: list,
		list2: err,
	})
}

// AddCreateResponse enqueues a new Create response.
func (m *Interface) AddCreateResponse(crd *apiext.CustomResourceDefinition, err error) {
	m.enqueueResponse(response{
		create1: crd,
		create2: err,
	})
}

// AddUpdateResponse enqueues a new Update response.
func (m *Interface) AddUpdateResponse(crd *apiext.CustomResourceDefinition, err error) {
	m.enqueueResponse(response{
		update1: crd,
		update2: err,
	})
}

// AddDeleteResponse enqueues a new Delete response.
func (m *Interface) AddDeleteResponse(err error) {
	m.enqueueResponse(response{
		delete1: err,
	})
}

// AddWatchResponse enqueues a new Watch response.
func (m *Interface) AddWatchResponse(w watch.Interface, err error) {
	m.enqueueResponse(response{
		watch1: w,
		watch2: err,
	})
}

// AddPanicResponse adds a response to the queue that causes a panic.
func (m *Interface) AddPanicResponse(s string) {
	m.enqueueResponse(response{
		panic: s,
	})
}

// String returns the current inncoming request log as string.
func (m *Interface) String() string {
	m.logLock.Lock()
	defer m.logLock.Unlock()

	return m.log
}

// Close the mock. If there are any pending events or listeners, there will be panic.
func (m *Interface) Close() {
	if len(m.responses) != 0 {
		panic(fmt.Sprintf("There still are pending undelivered pending responses\n%v", m.log))
	}

	if atomic.LoadInt32(&m.callers) != 0 {
		panic(fmt.Sprintf("There still are callers waiting for responses\n%v", m.log))
	}

	close(m.shutdown)
}

func (m *Interface) appendToLog(format string, args ...interface{}) {
	m.logLock.Lock()
	defer m.logLock.Unlock()

	str := fmt.Sprintf(format, args...)
	m.log += str
}

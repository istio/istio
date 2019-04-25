/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconciletest

import "sigs.k8s.io/controller-runtime/pkg/reconcile"

var _ reconcile.Reconciler = &FakeReconcile{}

// FakeReconcile implements reconcile.Reconciler by always returning Result and Err
type FakeReconcile struct {
	// Result is the result that will be returned by Reconciler
	Result reconcile.Result

	// Err is the error that will be returned by Reconciler
	Err error

	// If specified, Reconciler will write Requests to Chan
	Chan chan reconcile.Request
}

// Reconcile implements reconcile.Reconciler
func (f *FakeReconcile) Reconcile(r reconcile.Request) (reconcile.Result, error) {
	if f.Chan != nil {
		f.Chan <- r
	}
	return f.Result, f.Err
}

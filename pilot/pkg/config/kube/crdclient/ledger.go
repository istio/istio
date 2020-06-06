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

package crdclient

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pkg/ledger"

	"istio.io/istio/pilot/pkg/model"
)

func (cl *Client) Version() string {
	return cl.configLedger.RootHash()
}

func (cl *Client) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cl.configLedger.GetPreviousValue(version, key)
}

func (cl *Client) GetLedger() ledger.Ledger {
	return cl.configLedger
}

func (cl *Client) SetLedger(l ledger.Ledger) error {
	cl.configLedger = l
	return nil
}

func (cl *Client) tryLedgerPut(obj interface{}, kind string) {
	iobj := obj.(metav1.Object)
	key := model.Key(kind, iobj.GetName(), iobj.GetNamespace())
	_, err := cl.configLedger.Put(key, iobj.GetResourceVersion())
	if err != nil {
		scope.Errorf("Failed to update %s in ledger, status will be out of date.", key)
	}
}

func (cl *Client) tryLedgerDelete(obj interface{}, kind string) {
	iobj := obj.(metav1.Object)
	key := model.Key(kind, iobj.GetName(), iobj.GetNamespace())
	err := cl.configLedger.Delete(key)
	if err != nil {
		scope.Errorf("Failed to delete %s in ledger, status will be out of date.", key)
	}
}

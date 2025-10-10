// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package builder

import (
	"context"

	"sigs.k8s.io/knftables"

	"istio.io/istio/pkg/log"
)

// NftablesAPI defines the interface for interacting with nftables.
// It supports creating a transaction, running it, listing elements, and optionally dumping the config (mainly for testing).
type NftablesAPI interface {
	NewTransaction() *knftables.Transaction
	Run(ctx context.Context, tx *knftables.Transaction) error
	Dump(tx *knftables.Transaction) string
	// ListElements returns a list of the elements in a set or map. (objectType should be "set" or "map".)
	ListElements(ctx context.Context, objectType, name string) ([]*knftables.Element, error)
}

// NftImpl is the real implementation of NftablesAPI using the actual knftables backend.
type NftImpl struct {
	nft knftables.Interface
}

// Dump is used for logging purposes.
func (r *NftImpl) Dump(tx *knftables.Transaction) string {
	return tx.String()
}

// NewNftImpl creates and returns a NftImpl object.
// It sets up the actual knftables interface for the given family and table.
func NewNftImpl(family knftables.Family, table string) (*NftImpl, error) {
	nft, err := knftables.New(family, table)
	if err != nil {
		return nil, err
	}
	return &NftImpl{nft: nft}, nil
}

// NewTransaction starts a new transaction using the real knftables backend.
func (r *NftImpl) NewTransaction() *knftables.Transaction {
	return r.nft.NewTransaction()
}

// Run applies a transaction using the real knftables interface.
func (r *NftImpl) Run(ctx context.Context, tx *knftables.Transaction) error {
	return r.nft.Run(ctx, tx)
}

// ListElements returns a list of the elements in a set or map using the real knftables interface.
func (r *NftImpl) ListElements(ctx context.Context, objectType, name string) ([]*knftables.Element, error) {
	return r.nft.ListElements(ctx, objectType, name)
}

// MockNftables is a mock implementation of NftablesAPI for use in unit tests.
// It uses knftables.Fake to simulate nftables behavior without making changes to the system.
type MockNftables struct {
	*knftables.Fake
}

// NewMockNftables creates a new mock object with a fake backend. It is used in the unit tests.
func NewMockNftables(family knftables.Family, table string) *MockNftables {
	return &MockNftables{
		Fake: knftables.NewFake(family, table),
	}
}

// Dump returns the current mock table state as a string.
// We don't want to sort objects in the Dump result so we are not using the Fake.Dump method.
func (m *MockNftables) Dump(tx *knftables.Transaction) string {
	return tx.String()
}

// ListElements returns a list of the elements in a set or map using the mock knftables interface.
func (m *MockNftables) ListElements(ctx context.Context, objectType, name string) ([]*knftables.Element, error) {
	return m.Fake.ListElements(ctx, objectType, name)
}

func LogNftRules(rules *knftables.Transaction) {
	if rules.NumOperations() == 0 {
		log.Infof("There are no nftables rules to log")
		return
	}

	// For logging purposes, we can just use the transaction's String() method
	dump := rules.String()
	if dump != "" {
		log.Infof("nftables rules programmed:\n%s \n", dump)
	} else {
		log.Info("There are no nftables rules.")
	}
}

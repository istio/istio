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
package capture

import (
	"context"

	"sigs.k8s.io/knftables"
)

// NftablesAPI defines the interface for interacting with nftables.
// It supports creating a transaction, running it, and optionally dumping the config (mainly for testing).
type NftablesAPI interface {
	NewTransaction() *knftables.Transaction
	Run(ctx context.Context, tx *knftables.Transaction) error
	Dump(tx *knftables.Transaction) string
}

// RealNftables is the real implementation of NftablesAPI using the actual knftables backend.
type RealNftables struct {
	nft knftables.Interface
}

// Dump is part of the interface but not used in the real implementation. It's used as part of unit tests.
func (r *RealNftables) Dump(tx *knftables.Transaction) string {
	// We do not use Dump in the real Interface.
	return ""
}

// NewRealNftables creates and returns a RealNftables object.
// It sets up the actual knftables interface for the given family and table.
func NewRealNftables(family knftables.Family, table string) (*RealNftables, error) {
	nft, err := knftables.New(family, table)
	if err != nil {
		return nil, err
	}
	return &RealNftables{nft: nft}, nil
}

// NewTransaction starts a new transaction using the real knftables backend.
func (r *RealNftables) NewTransaction() *knftables.Transaction {
	return r.nft.NewTransaction()
}

// Run applies a transaction using the real knftables interface.
func (r *RealNftables) Run(ctx context.Context, tx *knftables.Transaction) error {
	return r.nft.Run(ctx, tx)
}

// MockNftables is a mock implementation of NftablesAPI for use in unit tests.
// It uses knftables.Fake to simulate nftables behavior without making changes to the system.
type MockNftables struct {
	*knftables.Fake
	DumpResults []string
}

// NewMockNftables creates a new mock object with a fake backend. It is used in the unit tests.
func NewMockNftables(family knftables.Family, table string) *MockNftables {
	return &MockNftables{
		Fake:        knftables.NewFake(family, table),
		DumpResults: make([]string, 0),
	}
}

// Dump returns the current mock table state as a string.
// We don't want to sort objects in the Dump result so we are not using the Fake.Dump method.
func (m *MockNftables) Dump(tx *knftables.Transaction) string {
	return tx.String()
}

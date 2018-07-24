// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package storage

import (
	"context"

	"github.com/open-policy-agent/opa/ast"
)

// Transaction defines the interface that identifies a consistent snapshot over
// the policy engine's storage layer.
type Transaction interface {
	ID() uint64
}

// Store defines the interface for the storage layer's backend.
type Store interface {
	Trigger
	Policy
	Indexing

	// NewTransaction is called create a new transaction in the store.
	NewTransaction(ctx context.Context, params ...TransactionParams) (Transaction, error)

	// Read is called to fetch a document referred to by path.
	Read(ctx context.Context, txn Transaction, path Path) (interface{}, error)

	// Write is called to modify a document referred to by path.
	Write(ctx context.Context, txn Transaction, op PatchOp, path Path, value interface{}) error

	// Commit is called to finish the transaction. If Commit returns an error, the
	// transaction must be automatically aborted by the Store implementation.
	Commit(ctx context.Context, txn Transaction) error

	// Abort is called to cancel the transaction.
	Abort(ctx context.Context, txn Transaction)
}

// TransactionParams describes a new transaction.
type TransactionParams struct {

	// Write indicates if this transaction will perform any write operations.
	Write bool
}

// WriteParams specifies the TransactionParams for a write transaction.
var WriteParams = TransactionParams{
	Write: true,
}

// PatchOp is the enumeration of supposed modifications.
type PatchOp int

// Patch supports add, remove, and replace operations.
const (
	AddOp     PatchOp = iota
	RemoveOp          = iota
	ReplaceOp         = iota
)

// WritesNotSupported provides a default implementation of the write
// interface which may be used if the backend does not support writes.
type WritesNotSupported struct{}

func (WritesNotSupported) Write(ctx context.Context, txn Transaction, op PatchOp, path Path, value interface{}) error {
	return writesNotSupportedError()
}

// Policy defines the interface for policy module storage.
type Policy interface {
	ListPolicies(context.Context, Transaction) ([]string, error)
	GetPolicy(context.Context, Transaction, string) ([]byte, error)
	UpsertPolicy(context.Context, Transaction, string, []byte) error
	DeletePolicy(context.Context, Transaction, string) error
}

// PolicyNotSupported provides a default implementation of the policy interface
// which may be used if the backend does not support policy storage.
type PolicyNotSupported struct{}

// ListPolicies always returns a PolicyNotSupportedErr.
func (PolicyNotSupported) ListPolicies(context.Context, Transaction) ([]string, error) {
	return nil, policyNotSupportedError()
}

// GetPolicy always returns a PolicyNotSupportedErr.
func (PolicyNotSupported) GetPolicy(context.Context, Transaction, string) ([]byte, error) {
	return nil, policyNotSupportedError()
}

// UpsertPolicy always returns a PolicyNotSupportedErr.
func (PolicyNotSupported) UpsertPolicy(context.Context, Transaction, string, []byte) error {
	return policyNotSupportedError()
}

// DeletePolicy always returns a PolicyNotSupportedErr.
func (PolicyNotSupported) DeletePolicy(context.Context, Transaction, string) error {
	return policyNotSupportedError()
}

// PolicyEvent describes a change to a policy.
type PolicyEvent struct {
	ID      string
	Data    []byte
	Removed bool
}

// DataEvent describes a change to a base data document.
type DataEvent struct {
	Path    Path
	Data    interface{}
	Removed bool
}

// TriggerEvent describes the changes that caused the trigger to be invoked.
type TriggerEvent struct {
	Policy []PolicyEvent
	Data   []DataEvent
}

// IsZero returns true if the TriggerEvent indicates no changes occurred. This
// function is primarily for test purposes.
func (e TriggerEvent) IsZero() bool {
	return !e.PolicyChanged() && !e.DataChanged()
}

// PolicyChanged returns true if the trigger was caused by a policy change.
func (e TriggerEvent) PolicyChanged() bool {
	return len(e.Policy) > 0
}

// DataChanged returns true if the trigger was caused by a data change.
func (e TriggerEvent) DataChanged() bool {
	return len(e.Data) > 0
}

// TriggerConfig contains the trigger registration configuration.
type TriggerConfig struct {

	// OnCommit is invoked when a transaction is successfully committed. The
	// callback is invoked with a handle to the write transaction that
	// successfully committed before other clients see the changes.
	OnCommit func(ctx context.Context, txn Transaction, event TriggerEvent)
}

// Trigger defines the interface that stores implement to register for change
// notifications when the store is changed.
type Trigger interface {
	Register(ctx context.Context, txn Transaction, config TriggerConfig) (TriggerHandle, error)
}

// TriggersNotSupported provides default implementations of the Trigger
// interface which may be used if the backend does not support triggers.
type TriggersNotSupported struct{}

// Register always returns an error indicating triggers are not supported.
func (TriggersNotSupported) Register(context.Context, Transaction, TriggerConfig) (TriggerHandle, error) {
	return nil, triggersNotSupportedError()
}

// TriggerHandle defines the interface that can be used to unregister triggers that have
// been registered on a Store.
type TriggerHandle interface {
	Unregister(ctx context.Context, txn Transaction)
}

// IndexIterator defines the interface for iterating over index results.
type IndexIterator func(*ast.ValueMap) error

// Indexing defines the interface for building an index.
type Indexing interface {
	Build(ctx context.Context, txn Transaction, ref ast.Ref) (Index, error)
}

// Index defines the interface for searching a pre-built index.
type Index interface {
	Lookup(ctx context.Context, txn Transaction, value interface{}, iter IndexIterator) error
}

// IndexingNotSupported provides default implementations of the Indexing
// interface which may be used if the backend does not support indexing.
type IndexingNotSupported struct{}

// Build always returns an error indicating indexing is not supported.
func (IndexingNotSupported) Build(context.Context, Transaction, ast.Ref) (Index, error) {
	return nil, indexingNotSupportedError()
}

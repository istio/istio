// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package inmem implements an in-memory version of the policy engine's storage
// layer.
//
// The in-memory store is used as the default storage layer implementation. The
// in-memory store supports multi-reader/single-writer concurrency with
// rollback.
//
// Callers should assume the in-memory store does not make copies of written
// data. Once data is written to the in-memory store, it should not be modified
// (outside of calling Store.Write). Furthermore, data read from the in-memory
// store should be treated as read-only.
package inmem

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
)

// New returns an empty in-memory store.
func New() storage.Store {
	return &store{
		data:     map[string]interface{}{},
		triggers: map[*handle]storage.TriggerConfig{},
		policies: map[string][]byte{},
		indices:  newIndices(),
	}
}

// NewFromObject returns a new in-memory store from the supplied data object.
func NewFromObject(data map[string]interface{}) storage.Store {
	db := New()
	ctx := context.Background()
	txn, err := db.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		panic(err)
	}
	if err := db.Write(ctx, txn, storage.AddOp, storage.Path{}, data); err != nil {
		panic(err)
	}
	if err := db.Commit(ctx, txn); err != nil {
		panic(err)
	}
	return db
}

// NewFromReader returns a new in-memory store from a reader that produces a
// JSON serialized object. This function is for test purposes.
func NewFromReader(r io.Reader) storage.Store {
	d := util.NewJSONDecoder(r)
	var data map[string]interface{}
	if err := d.Decode(&data); err != nil {
		panic(err)
	}
	return NewFromObject(data)
}

type store struct {
	rmu      sync.RWMutex                      // reader-writer lock
	wmu      sync.Mutex                        // writer lock
	xid      uint64                            // last generated transaction id
	data     map[string]interface{}            // raw data
	policies map[string][]byte                 // raw policies
	triggers map[*handle]storage.TriggerConfig // registered triggers
	indices  *indices                          // data ref indices
}

type handle struct {
	db *store
}

func (db *store) NewTransaction(ctx context.Context, params ...storage.TransactionParams) (storage.Transaction, error) {
	var write bool
	if len(params) > 0 {
		write = params[0].Write
	}
	xid := atomic.AddUint64(&db.xid, uint64(1))
	if write {
		db.wmu.Lock()
	} else {
		db.rmu.RLock()
	}
	return newTransaction(xid, write, db), nil
}

func (db *store) Commit(ctx context.Context, txn storage.Transaction) error {
	underlying := txn.(*transaction)
	if underlying.write {
		db.rmu.Lock()
		event := underlying.Commit()
		db.indices = newIndices()
		db.runOnCommitTriggers(ctx, txn, event)
		db.rmu.Unlock()
		db.wmu.Unlock()
	} else {
		db.rmu.RUnlock()
	}
	return nil
}

func (db *store) Abort(ctx context.Context, txn storage.Transaction) {
	underlying := txn.(*transaction)
	if underlying.write {
		db.wmu.Unlock()
	} else {
		db.rmu.RUnlock()
	}
}

func (db *store) ListPolicies(_ context.Context, txn storage.Transaction) ([]string, error) {
	underlying := txn.(*transaction)
	return underlying.ListPolicies(), nil
}

func (db *store) GetPolicy(_ context.Context, txn storage.Transaction, id string) ([]byte, error) {
	underlying := txn.(*transaction)
	return underlying.GetPolicy(id)
}

func (db *store) UpsertPolicy(_ context.Context, txn storage.Transaction, id string, bs []byte) error {
	underlying := txn.(*transaction)
	return underlying.UpsertPolicy(id, bs)
}

func (db *store) DeletePolicy(_ context.Context, txn storage.Transaction, id string) error {
	underlying := txn.(*transaction)
	if _, err := underlying.GetPolicy(id); err != nil {
		return err
	}
	return underlying.DeletePolicy(id)
}

func (db *store) Register(ctx context.Context, txn storage.Transaction, config storage.TriggerConfig) (storage.TriggerHandle, error) {
	underlying := txn.(*transaction)
	if !underlying.write {
		return nil, &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "triggers must be registered with a write transaction",
		}
	}
	h := &handle{db}
	db.triggers[h] = config
	return h, nil
}

func (db *store) Read(ctx context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	underlying := txn.(*transaction)
	return underlying.Read(path)
}

func (db *store) Write(ctx context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, value interface{}) error {
	underlying := txn.(*transaction)
	return underlying.Write(op, path, value)
}

func (db *store) Build(ctx context.Context, txn storage.Transaction, ref ast.Ref) (storage.Index, error) {
	underlying := txn.(*transaction)
	if underlying.write {
		return nil, &storage.Error{
			Code:    storage.IndexingNotSupportedErr,
			Message: "in-memory store does not support indexing on write transactions",
		}
	}
	return db.indices.Build(ctx, db, txn, ref)
}

func (h *handle) Unregister(ctx context.Context, txn storage.Transaction) {
	underlying := txn.(*transaction)
	if !underlying.write {
		return
	}
	delete(h.db.triggers, h)
}

func (db *store) runOnCommitTriggers(ctx context.Context, txn storage.Transaction, event storage.TriggerEvent) {
	for _, t := range db.triggers {
		t.OnCommit(ctx, txn, event)
	}
}

var doesNotExistMsg = "document does not exist"
var rootMustBeObjectMsg = "root must be object"
var rootCannotBeRemovedMsg = "root cannot be removed"
var conflictMsg = "value conflict"
var outOfRangeMsg = "array index out of range"
var arrayIndexTypeMsg = "array index must be integer"
var corruptPolicyMsg = "corrupt policy found"

func internalError(f string, a ...interface{}) *storage.Error {
	return &storage.Error{
		Code:    storage.InternalErr,
		Message: fmt.Sprintf(f, a...),
	}
}

func invalidPatchError(f string, a ...interface{}) *storage.Error {
	return &storage.Error{
		Code:    storage.InvalidPatchErr,
		Message: fmt.Sprintf(f, a...),
	}
}

func notFoundError(path storage.Path) *storage.Error {
	return notFoundErrorHint(path, doesNotExistMsg)
}

func notFoundErrorHint(path storage.Path, hint string) *storage.Error {
	return notFoundErrorf("%v: %v", path.String(), hint)
}

func notFoundErrorf(f string, a ...interface{}) *storage.Error {
	msg := fmt.Sprintf(f, a...)
	return &storage.Error{
		Code:    storage.NotFoundErr,
		Message: msg,
	}
}

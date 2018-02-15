// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package storage

import (
	"context"
)

// NewTransactionOrDie is a helper function to create a new transaction. If the
// storage layer cannot create a new transaction, this function will panic. This
// function should only be used for tests.
func NewTransactionOrDie(ctx context.Context, store Store, params ...TransactionParams) Transaction {
	txn, err := store.NewTransaction(ctx, params...)
	if err != nil {
		panic(err)
	}
	return txn
}

// ReadOne is a convenience function to read a single value from the provided Store. It
// will create a new Transaction to perform the read with, and clean up after itself
// should an error occur.
func ReadOne(ctx context.Context, store Store, path Path) (interface{}, error) {
	txn, err := store.NewTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Abort(ctx, txn)

	return store.Read(ctx, txn, path)
}

// WriteOne is a convenience function to write a single value to the provided Store. It
// will create a new Transaction to perform the write with, and clean up after itself
// should an error occur.
func WriteOne(ctx context.Context, store Store, op PatchOp, path Path, value interface{}) error {
	txn, err := store.NewTransaction(ctx, WriteParams)
	if err != nil {
		return err
	}

	if err := store.Write(ctx, txn, op, path, value); err != nil {
		store.Abort(ctx, txn)
		return err
	}

	return store.Commit(ctx, txn)
}

// Txn is a convenience function that executes f inside a new transaction
// opened on the store. If the function returns an error, the transaction is
// aborted and the error is returned. Otherwise, the transaction is committed
// and the result of the commit is returned.
func Txn(ctx context.Context, store Store, params TransactionParams, f func(Transaction) error) error {

	txn, err := store.NewTransaction(ctx, params)
	if err != nil {
		return err
	}

	if err := f(txn); err != nil {
		store.Abort(ctx, txn)
		return err
	}

	return store.Commit(ctx, txn)
}

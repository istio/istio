package model

import (
	"errors"

	"istio.io/pkg/ledger"
)

// DisabledLedger is an empty mock of the ledger.Ledger interface
// which we will substitute when distribution tracking is disabled.
type DisabledLedger struct {
	ledger.Ledger
}

func (d *DisabledLedger) Put(key, value string) (string, error) {
	return "", nil
}
func (d *DisabledLedger) Delete(key string) error {
	return nil
}
func (d *DisabledLedger) Get(key string) (string, error) {
	return "", nil
}
func (d *DisabledLedger) RootHash() string {
	return ""
}
func (d *DisabledLedger) GetPreviousValue(previousHash, key string) (result string, err error) {
	return "", errors.New("distribution tracking is disabled")
}

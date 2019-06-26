package jwk

import "github.com/pkg/errors"

func (ops *KeyOperationList) Get() KeyOperationList {
	if ops == nil {
		return nil
	}
	return *ops
}

func (ops *KeyOperationList) Accept(v interface{}) error {
	switch x := v.(type) {
	case string:
		return ops.Accept([]string{x})
	case []interface{}:
		l := make([]string, len(x))
		for i, e := range x {
			if es, ok := e.(string); ok {
				l[i] = es
			} else {
				return errors.Errorf(`invalid list element type: expected string, got %T`, v)
			}
		}
		return ops.Accept(l)
	case []string:
		list := make([]KeyOperation, len(x))
		for i, e := range x {
			switch e := KeyOperation(e); e {
			case KeyOpSign, KeyOpVerify, KeyOpEncrypt, KeyOpDecrypt, KeyOpWrapKey, KeyOpUnwrapKey, KeyOpDeriveKey, KeyOpDeriveBits:
				list[i] = e
			default:
				return errors.Errorf(`invalid keyoperation %v`, e)
			}
		}

		*ops = list
		return nil
	case KeyOperationList:
		*ops = x
		return nil
	default:
		return errors.Errorf(`invalid value %T`, v)
	}
}

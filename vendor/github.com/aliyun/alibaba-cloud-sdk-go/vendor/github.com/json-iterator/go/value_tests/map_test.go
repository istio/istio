package test

import (
	"encoding/json"
	"math/big"
)

func init() {
	var pRawMessage = func(val json.RawMessage) *json.RawMessage {
		return &val
	}
	nilMap := map[string]string(nil)
	marshalCases = append(marshalCases,
		map[string]interface{}{"abc": 1},
		map[string]MyInterface{"hello": MyString("world")},
		map[*big.Float]string{big.NewFloat(1.2): "2"},
		map[string]interface{}{
			"3": 3,
			"1": 1,
			"2": 2,
		},
		map[uint64]interface{}{
			uint64(1): "a",
			uint64(2): "a",
			uint64(4): "a",
		},
		nilMap,
		&nilMap,
		map[string]*json.RawMessage{"hello": pRawMessage(json.RawMessage("[]"))},
	)
	unmarshalCases = append(unmarshalCases, unmarshalCase{
		ptr:   (*map[string]string)(nil),
		input: `{"k\"ey": "val"}`,
	}, unmarshalCase{
		ptr:   (*map[string]string)(nil),
		input: `null`,
	}, unmarshalCase{
		ptr:   (*map[string]*json.RawMessage)(nil),
		input: "{\"test\":[{\"key\":\"value\"}]}",
	})
}

type MyInterface interface {
	Hello() string
}

type MyString string

func (ms MyString) Hello() string {
	return string(ms)
}

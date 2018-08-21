package test

import "encoding/json"

func init() {
	marshalCases = append(marshalCases,
		json.RawMessage("{}"),
	)
	unmarshalCases = append(unmarshalCases, unmarshalCase{
		ptr:   (*json.RawMessage)(nil),
		input: `[1,2,3]`,
	})
}

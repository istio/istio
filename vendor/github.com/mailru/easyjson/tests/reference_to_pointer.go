package tests

type Struct1 struct {
}

//easyjson:json
type Struct2 struct {
	From    *Struct1 `json:"from,omitempty"`
	Through *Struct1 `json:"through,omitempty"`
}

package tests

//easyjson:json
type DisallowUnknown struct {
	FieldOne string `json:"field_one"`
}

var disallowUnknownString = `{"field_one": "one", "field_two": "two"}`

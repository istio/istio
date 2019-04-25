package tests

type KeyWithEncodingMarshaler int

func (f KeyWithEncodingMarshaler) MarshalText() (text []byte, err error) {
	return []byte("hello"), nil
}

func (f *KeyWithEncodingMarshaler) UnmarshalText(text []byte) error {
	if string(text) == "hello" {
		*f = 5
	}
	return nil
}

//easyjson:json
type KeyWithEncodingMarshalers map[KeyWithEncodingMarshaler]string

var mapWithEncodingMarshaler KeyWithEncodingMarshalers = KeyWithEncodingMarshalers{5: "hello"}
var mapWithEncodingMarshalerString = `{"hello":"hello"}`

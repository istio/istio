package responses

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestFuzzyFieldUnmarshal(t *testing.T) {
	from, err := getJsonBytes()
	if err != nil {
		panic(err)
	}
	fmt.Println("From:")
	fmt.Println(string(from))
	to := &To{}
	// support auto json type trans
	initJsonParserOnce()
	err = jsonParser.Unmarshal(from, to)
	if err != nil {
		panic(err)
	}

	fmt.Println("To:")
	bytesTo, _ := json.MarshalIndent(to, "", "    ")
	fmt.Println(string(bytesTo))

	assert.Equal(t, "demo string", to.StrToStr)
	assert.Equal(t, 123, to.StrToInt)
	assert.Equal(t, int64(2147483648), to.StrToInt64)
	assert.Equal(t, 1.23, to.StrToFloat)
	assert.True(t, to.UpperFullStrToBool)
	assert.True(t, to.UpperCamelStrToBool)
	assert.True(t, to.LowerStrToBool)

	assert.Equal(t, "123", to.IntToStr)
	assert.Equal(t, 123, to.IntToInt)

	assert.Equal(t, "2147483648", to.Int64ToStr)
	assert.Equal(t, int64(2147483648), to.Int64ToInt64)

	assert.Equal(t, "true", to.BoolToStr)
	assert.Equal(t, true, to.BoolToBool)

	assert.Equal(t, 0, to.EmptyStrToInt)
	assert.Equal(t, int64(0), to.EmptyStrToInt64)
	assert.Equal(t, float64(0), to.EmptyStrToFloat)
	assert.Equal(t, false, to.EmptyStrToBool)
	assert.Equal(t, "", to.EmptyStrToStr)
}

func getJsonBytes() ([]byte, error) {
	from := &From{
		StrToStr:            "demo string",
		StrToInt:            "123",
		StrToInt64:          "2147483648",
		StrToFloat:          "1.23",
		UpperFullStrToBool:  "TRUE",
		UpperCamelStrToBool: "True",
		LowerStrToBool:      "true",

		IntToStr: 123,
		IntToInt: 123,

		Int64ToStr:   int64(2147483648),
		Int64ToInt64: int64(2147483648),

		FloatToStr:   float64(1.23),
		FloatToFloat: float64(1.23),

		BoolToStr:  true,
		BoolToBool: true,

		EmptyStrToInt:   "",
		EmptyStrToInt64: "",
		EmptyStrToFloat: "",
		EmptyStrToBool:  "",
		EmptyStrToStr:   "",
	}

	return json.MarshalIndent(from, "", "    ")
}

type From struct {
	StrToStr            string
	StrToInt            string
	StrToInt64          string
	StrToFloat          string
	UpperFullStrToBool  string
	UpperCamelStrToBool string
	LowerStrToBool      string

	IntToStr int
	IntToInt int

	Int64ToStr   int64
	Int64ToInt64 int64

	FloatToStr   float64
	FloatToFloat float64

	BoolToStr  bool
	BoolToBool bool

	EmptyStrToInt   string
	EmptyStrToInt64 string
	EmptyStrToFloat string
	EmptyStrToBool  string
	EmptyStrToStr   string
}

type To struct {
	StrToStr            string
	StrToInt            int
	StrToInt64          int64
	StrToFloat          float64
	UpperFullStrToBool  bool
	UpperCamelStrToBool bool
	LowerStrToBool      bool

	IntToStr string
	IntToInt int

	Int64ToStr   string
	Int64ToInt64 int64

	FloatToStr   string
	FloatToFloat float64

	BoolToStr  string
	BoolToBool bool

	EmptyStrToInt   int
	EmptyStrToInt64 int64
	EmptyStrToFloat float64
	EmptyStrToBool  bool
	EmptyStrToStr   string

	NilToInt   int
	NilToInt64 int64
	NilToFloat float64
	NilToBool  bool
	NilToStr   string
}

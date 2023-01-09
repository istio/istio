package hash

import (
	"testing"
)

func TestFactory(t *testing.T) {
	testCases := []struct {
		name                   string
		str                    string
		wantSum                []byte
		wantStr                string
		wantBigEndianUint64    uint64
		wantLittleEndianUint64 uint64
	}{
		{
			name: "foo",
			str:  "foo",
			// note: different hash implementations may get different hash value
			wantSum:                []byte{51, 191, 0, 168, 89, 196, 186, 63},
			wantStr:                "33bf00a859c4ba3f",
			wantBigEndianUint64:    3728699739546630719,
			wantLittleEndianUint64: 4592198659407396659,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			h := New()
			h.Write([]byte(tt.str))
			if gotSum := h.Sum(nil); string(tt.wantSum) != string(gotSum) {
				t.Errorf("wantSum %v, but got %v", tt.wantSum, gotSum)
			}
			if gotStr := h.ToString(nil); tt.wantStr != gotStr {
				t.Errorf("wantStr %v, but got %v", tt.wantStr, gotStr)
			}
			if gotUint64 := h.ToBigEndianUint64(nil); tt.wantBigEndianUint64 != gotUint64 {
				t.Errorf("wantBigEndianUint64 %v, but got %v", tt.wantBigEndianUint64, gotUint64)
			}
			if gotUint64 := h.ToLittleEndianUint64(nil); tt.wantLittleEndianUint64 != gotUint64 {
				t.Errorf("wantLittleEndianUint64 %v, but got %v", tt.wantLittleEndianUint64, gotUint64)
			}
		})
	}
}

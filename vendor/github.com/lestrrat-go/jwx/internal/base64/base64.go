package base64

import (
	"encoding/base64"
	"encoding/binary"
)

func EncodeToString(src []byte) string {
	return base64.RawURLEncoding.EncodeToString(src)
}

func EncodeUint64ToString(v uint64) string {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, v)

	i := 0
	for ; i < len(data); i++ {
		if data[i] != 0x0 {
			break
		}
	}

	return EncodeToString(data[i:])
}

func DecodeString(src string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(src)
}

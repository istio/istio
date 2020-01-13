package bs

import (
	"reflect"
	"unsafe"
)

// String2Bytes converts string to byte slice without a memory allocation.
func String2Bytes(s string) (b []byte) {
	sh := *(*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	bh.Data, bh.Len, bh.Cap = sh.Data, sh.Len, sh.Len
	return b
}

// Bytes2String converts byte slice to string without a memory allocation.
func Bytes2String(s []byte) string {
	return *(*string)(unsafe.Pointer(&s))
}

package validation

import (
	"encoding/binary"
	"testing"

	"github.com/coreos/etcd/pkg/cpuutil"
)

func TestNtohs(t *testing.T) {
	hostValue := ntohs(0xbeef)
	expectValue := 0xbeef
	if cpuutil.ByteOrder().String() == binary.LittleEndian.String() {
		expectValue = 0xefbe
	}
	if hostValue != uint16(expectValue) {
		t.Errorf("Expected evaluating ntohs(%v) is %v, actual %v", 0xbeef, expectValue, hostValue)
	}
}

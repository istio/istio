package validation_test

import (
	"errors"
	"net"
	"strconv"
	"testing"

	"istio.io/istio/tools/ambient/pkg/validation"
)

func TestOpenPortValidator(t *testing.T) {

	// start ipv4 listening port
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		_, portString, err := net.SplitHostPort(l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}

		port, err := strconv.Atoi(portString)
		if err != nil {
			t.Fatal(err)
		}

		opv := validation.NewOpenPortValidator(uint16(port))
		if err := opv.Validate(); err != nil {
			t.Fatal(err)
		}

		l.Close()
		if err := opv.Validate(); !errors.Is(err, validation.ErrorPortNotOpen) {
			t.Fatal("expected error")
		}
	}

}

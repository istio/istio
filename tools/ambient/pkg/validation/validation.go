package validation

import (
	"errors"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"istio.io/istio/cni/pkg/ambient/constants"
)

var (
	ErrorPortNotOpen = errors.New("listening port was not found")
)

func NewOpenPortValidator(port uint16) *OpenPortValidator {
	return &OpenPortValidator{
		port: port,
	}
}

type OpenPortValidator struct {
	port uint16
}

func (o *OpenPortValidator) Validate() error {
	socks4, err1 := netlink.SocketDiagTCP(unix.AF_INET)
	socks6, err2 := netlink.SocketDiagTCP(unix.AF_INET6)
	if err1 != nil && err2 != nil {
		return errors.Join(err1, err2)
	}
	const TCP_LISTEN = 10
	for _, sock := range append(socks4, socks6...) {
		if sock.State != TCP_LISTEN {
			continue
		}
		if sock.ID.SourcePort == o.port {
			return nil
		}
	}

	return ErrorPortNotOpen
}

func isZtunnelPresent() error {
	return NewOpenPortValidator(constants.ZtunnelInboundPort).Validate()
}

func RunAmbientCheck() {
	for {
		if err := isZtunnelPresent(); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}

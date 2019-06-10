package dependencies

import (
	"net"
	"os/user"
)

type Cmd struct {
	command string
}

var (
	IPTABLES       = Cmd{"iptables"}
	IPTABLES_SAVE  = Cmd{"iptables-save"}
	IP6TABLES      = Cmd{"ip6tables"}
	IP6TABLES_SAVE = Cmd{"ip6tables-save"}
	IP             = Cmd{"ip"}
)

type Dependencies interface {
	GetLocalIP() (net.IP, error)
	LookupUser() (*user.User, error)
	RunOrFail(cmd Cmd, args ...string)
	Run(cmd Cmd, args ...string) error
	RunQuietlyAndIgnore(cmd Cmd, args ...string)
}

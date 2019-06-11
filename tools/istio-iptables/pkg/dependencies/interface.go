package dependencies

import (
	"net"
	"os/user"
)

type Cmd string

const (
	IPTABLES      = "iptables"
	IPTABLESSAVE  = "iptables-save"
	IP6TABLES     = "ip6tables"
	IP6TABLESSAVE = "ip6tables-save"
	IP            = "ip"
)

type Dependencies interface {
	GetLocalIP() (net.IP, error)
	LookupUser() (*user.User, error)
	RunOrFail(cmd Cmd, args ...string)
	Run(cmd Cmd, args ...string) error
	RunQuietlyAndIgnore(cmd Cmd, args ...string)
}

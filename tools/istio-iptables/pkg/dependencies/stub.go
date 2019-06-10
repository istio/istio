package dependencies

import (
	"fmt"
	"net"
	"os/user"
	"strings"
)

type StdoutStubDependencies struct {
	as string
}

func (s *StdoutStubDependencies) GetLocalIP() (net.IP, error) {
	fmt.Println("ip")
	return net.IPv4(127, 0, 0, 1), nil
}

func (s *StdoutStubDependencies) LookupUser() (*user.User, error) {
	fmt.Println("id")
	return nil, nil
}

func (s *StdoutStubDependencies) RunOrFail(cmd Cmd, args ...string) {
	fmt.Printf("%s %s\n", cmd.command, strings.Join(args, " "))
}

func (s *StdoutStubDependencies) Run(cmd Cmd, args ...string) error {
	fmt.Printf("%s %s\n", cmd.command, strings.Join(args, " "))
	return nil
}

func (s *StdoutStubDependencies) RunQuietlyAndIgnore(cmd Cmd, args ...string) {
	fmt.Printf("%s %s\n", cmd.command, strings.Join(args, " "))
}

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"
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

type Command struct {
	Command          string
	Args             []string
	RedirectToStdErr bool
}

func defaultCommandRunner(command Command) error {
	fmt.Printf("%s %s\n", command.Command, strings.Join(command.Args, " "))
	cmd := exec.Command(command.Command, command.Args...)
	cmd.Stdout = os.Stdout
	if !command.RedirectToStdErr {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

var commandRunner = defaultCommandRunner

func (i Command) Run() error {
	return commandRunner(i)
}

func (i Command) RunOrFail() {
	err := i.Run()
	if err != nil {
		panic(err)
	}
}

func (i Command) RunQuietlyAndIgnore() {
	i.RedirectToStdErr = true
	i.Run()
}

type Dependencies interface {
	GetLocalIP() (net.IP, error)
	LookupUser() (*user.User, error)
	RunOrFail(cmd Cmd, args ...string)
	Run(cmd Cmd, args ...string) error
	RunQuietlyAndIgnore(cmd Cmd, args ...string)
}

type RealDependencies struct {
	as string
}

func (r *RealDependencies) GetLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			return ipnet.IP, nil
		}
	}
	return nil, fmt.Errorf("no valid local IP address found")
}

func (r *RealDependencies) LookupUser() (*user.User, error) {
	return user.Lookup(getEnvWithDefault("ENVOY_USER", "istio-proxy"))
}

func (r *RealDependencies) RunOrFail(cmd Cmd, args ...string) {
	Command{Command: cmd.command, Args: args, RedirectToStdErr: false}.RunOrFail()
}

func (r *RealDependencies) Run(cmd Cmd, args ...string) error {
	return Command{Command: cmd.command, Args: args, RedirectToStdErr: false}.Run()
}

func (r *RealDependencies) RunQuietlyAndIgnore(cmd Cmd, args ...string) {
	Command{Command: cmd.command, Args: args, RedirectToStdErr: false}.RunQuietlyAndIgnore()
}

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

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
	IPTABLES  = Cmd{"iptables"}
	IP6TABLES = Cmd{"ip6tables"}
	IP        = Cmd{"ip"}
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

func (r *RealDependencies) Iptables(args ...string) {
	Command{Command: "iptables", Args: args, RedirectToStdErr: false}.RunOrFail()
}

func (r *RealDependencies) TryIptables(args ...string) error {
	return Command{Command: "iptables", Args: args, RedirectToStdErr: false}.Run()
}

func (r *RealDependencies) Ip6tables(args ...string) {
	Command{Command: "ip6tables", Args: args, RedirectToStdErr: false}.RunOrFail()
}

func (r *RealDependencies) Ip6tablesQuietly(args ...string) {
	c := Command{Command: "ip6tables", Args: args, RedirectToStdErr: false}
	c.RunQuietlyAndIgnore()
}

func (r *RealDependencies) Ip(args ...string) {
	Command{Command: "ip", Args: args, RedirectToStdErr: false}.RunOrFail()
}

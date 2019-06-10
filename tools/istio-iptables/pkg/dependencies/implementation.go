package dependencies

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

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

func getEnvWithDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func (r *RealDependencies) LookupUser() (*user.User, error) {
	return user.Lookup(getEnvWithDefault("ENVOY_USER", "istio-proxy"))
}

func (r *RealDependencies) execute(cmd Cmd, redirectStdout bool, args ...string) error {
	fmt.Printf("%s %s\n", cmd.command, strings.Join(args, " "))
	externalCommand := exec.Command(cmd.command, args...)
	externalCommand.Stdout = os.Stdout
	//TODO Check naming and redirection logic
	if !redirectStdout {
		externalCommand.Stderr = os.Stderr
	}
	return externalCommand.Run()
}

func (r *RealDependencies) RunOrFail(cmd Cmd, args ...string) {
	err := r.execute(cmd, false, args...)

	if err != nil {
		panic(err)
	}
}

func (r *RealDependencies) Run(cmd Cmd, args ...string) error {
	return r.execute(cmd, false, args...)
}

func (r *RealDependencies) RunQuietlyAndIgnore(cmd Cmd, args ...string) {
	r.execute(cmd, true, args...)
}

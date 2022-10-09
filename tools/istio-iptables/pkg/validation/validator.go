// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validation

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/pkg/log"
)

var istioLocalIPv6 = net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 6}

type ReturnCode int

const (
	DONE ReturnCode = iota
)

type Validator struct {
	Config *Config
}

type Config struct {
	ServerListenAddress []string
	ServerOriginalPort  uint16
	ServerOriginalIP    net.IP
	ServerReadyBarrier  chan ReturnCode
	ProbeTimeout        time.Duration
}

type Service struct {
	Config *Config
}

type Client struct {
	Config *Config
}

func (validator *Validator) Run() error {
	log.Infof("Starting iptables validation. This check verifies that iptables rules are properly established for the network.")
	s := Service{
		validator.Config,
	}
	sError := make(chan error, 1)
	sTimer := time.NewTimer(s.Config.ProbeTimeout)
	defer sTimer.Stop()
	go func() {
		sError <- s.Run()
	}()

	// infinite loop
	go func() {
		c := Client{Config: validator.Config}
		<-c.Config.ServerReadyBarrier
		for {
			_ = c.Run()
			// Avoid spamming the request to the validation server.
			// Since the TIMEWAIT socket is cleaned up in 60 second,
			// it's maintaining 60 TIMEWAIT sockets. Not big deal.
			time.Sleep(time.Second)
		}
	}()
	select {
	case <-sTimer.C:
		return fmt.Errorf("validation timeout")
	case err := <-sError:
		if err == nil {
			log.Info("Validation passed, iptables rules established")
		} else {
			log.Errorf("Validation failed: %v", err)
		}
		return err
	}
}

// TODO(lambdai): remove this if iptables only need to redirect to outbound proxy port on A call A
func genListenerAddress(ip net.IP, ports []string) []string {
	addresses := make([]string, 0, len(ports))
	for _, port := range ports {
		addresses = append(addresses, net.JoinHostPort(ip.String(), port))
	}
	return addresses
}

func NewValidator(config *config.Config, hostIP net.IP) *Validator {
	// It's tricky here:
	// Connect to 127.0.0.6 will redirect to 127.0.0.1
	// Connect to ::6       will redirect to ::1
	isIpv6 := hostIP.To4() == nil
	listenIP := net.IPv4(127, 0, 0, 1)
	serverIP := net.IPv4(127, 0, 0, 6)
	if isIpv6 {
		listenIP = net.IPv6loopback
		serverIP = istioLocalIPv6
	}
	return &Validator{
		Config: &Config{
			ServerListenAddress: genListenerAddress(listenIP, []string{config.ProxyPort, config.InboundCapturePort}),
			ServerOriginalPort:  config.IptablesProbePort,
			ServerOriginalIP:    serverIP,
			ServerReadyBarrier:  make(chan ReturnCode, 1),
			ProbeTimeout:        config.ProbeTimeout,
		},
	}
}

// Write human readable response
func echo(conn io.WriteCloser, echo []byte) {
	_, _ = conn.Write(echo)
	_ = conn.Close()
}

func restoreOriginalAddress(l net.Listener, config *Config, c chan<- ReturnCode) {
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Errorf("Listener failed to accept connection: %v", err)
			continue
		}
		_, port, err := GetOriginalDestination(conn)
		if err != nil {
			log.Errorf("Error getting original dst: %v", err)
			conn.Close()
			continue
		}

		// echo original port for debugging.
		// Since the write amount is small it should fit in sock buffer and never blocks.
		echo(conn, []byte(strconv.Itoa(int(port))))
		// Handle connections
		// Since the write amount is small it should fit in sock buffer and never blocks.
		if port != config.ServerOriginalPort {
			// This could be probe request from no where
			continue
		}
		// Server recovers the magical original port
		c <- DONE
		return
	}
}

func (s *Service) Run() error {
	// at most 2 message: ipv4 and ipv6
	c := make(chan ReturnCode, 2)
	hasAtLeastOneListener := false
	for _, addr := range s.Config.ServerListenAddress {
		log.Infof("Listening on %v", addr)
		config := &net.ListenConfig{Control: reuseAddr}

		l, err := config.Listen(context.Background(), "tcp", addr) // bind to the address:port
		if err != nil {
			log.Errorf("Error on listening: %v", err)
			continue
		}

		hasAtLeastOneListener = true
		go restoreOriginalAddress(l, s.Config, c)
	}
	if hasAtLeastOneListener {
		s.Config.ServerReadyBarrier <- DONE
		// bump at least one since we currently support either v4 or v6
		<-c
		return nil
	}
	return fmt.Errorf("no listener available: %s", strings.Join(s.Config.ServerListenAddress, ","))
}

func (c *Client) Run() error {
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	if c.Config.ServerOriginalIP.To4() == nil {
		laddr, err = net.ResolveTCPAddr("tcp", "[::1]:0")
		if err != nil {
			return err
		}
	}
	sOriginalPort := fmt.Sprintf("%d", c.Config.ServerOriginalPort)
	serverOriginalAddress := net.JoinHostPort(c.Config.ServerOriginalIP.String(), sOriginalPort)
	raddr, err := net.ResolveTCPAddr("tcp", serverOriginalAddress)
	if err != nil {
		return err
	}
	conn, err := net.DialTCP("tcp", laddr, raddr)
	if err != nil {
		log.Errorf("Error connecting to %s: %v", serverOriginalAddress, err)
		return err
	}
	conn.Close()
	return nil
}

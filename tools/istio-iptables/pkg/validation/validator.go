package validation

import (
	"errors"
	"fmt"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"net"
	"strconv"
	"time"
)

type Validator struct {
	Config *ValidationConfig
}

type ValidationConfig struct {
	ServerListenAddress string
	ServerOriginalPort  uint16
	ServerOriginalIp    net.IP
}
type ValidationService struct {
	Config *ValidationConfig
}
type ValidationClient struct {
	Config *ValidationConfig
}

func (validator *Validator) Run() error {
	s := ValidationService{
		validator.Config,
	}
	sError := make(chan error, 1)
	sTimeout := time.After(30 * time.Second)
	go func() {
		sError <- s.Run()
	}()

	// infinite loop
	go func() {
		c := ValidationClient{Config: validator.Config}
		for {
			c.Run()
			// Avoid spamming the request to the validation server.
			// Since the TIMEWAIT socket is cleaned up in 60 second,
			// it's maintaining 60 TIMEWAIT sockets. Not big deal.
			time.Sleep(time.Second)
		}
	}()
	select {
	case <-sTimeout:
		return errors.New("validation timeout")
	case err := <-sError:
		if err == nil {
			fmt.Println("validation passed!")
		} else {
			fmt.Println("validation failed:" + err.Error())
		}
		return err
	}
}

func NewValidator(config *config.Config) *Validator {
	return &Validator{
		Config: &ValidationConfig{
			ServerListenAddress: ":" + config.InboundCapturePort,
			ServerOriginalPort:  constants.IPTABLES_PROBE_PORT,
			ServerOriginalIp:    config.HostIp,
		},
	}
}

// Write human readable response
func echo(conn net.Conn, echo []byte) {
	conn.Write(echo)
	conn.Close()
}

func (s *ValidationService) Run() error {
	l, err := net.Listen("tcp", s.Config.ServerListenAddress)
	if err != nil {
		fmt.Println("Error on listening:", err.Error())
		return err
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on " + s.Config.ServerListenAddress)
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			continue
		}
		_, port, err := GetOriginalDestination(conn)
		if err != nil {
			fmt.Println("Error getting original dst: " + err.Error())
			conn.Close()
			continue
		}

		// echo original port for debugging.
		// Since the write amount is small it should fit in sock buffer and never blocks.
		echo(conn, []byte(strconv.Itoa(int(port))))
		// Handle connections
		// Since the write amount is small it should fit in sock buffer and never blocks.
		if port != s.Config.ServerOriginalPort {
			// This could be probe request from no where
			continue
		}
		// Server recovers the magical original port
		return nil
	}
}

func (c *ValidationClient) Run() error {
	serverOriginalAddress := c.Config.ServerOriginalIp.String() + ":" + strconv.Itoa(int(c.Config.ServerOriginalPort))
	conn, err := net.Dial("tcp", serverOriginalAddress)
	if err != nil {
		fmt.Printf("Error connecting to %s: %s\n", serverOriginalAddress, err.Error())
		return err
	}
	conn.Close()
	return nil
}

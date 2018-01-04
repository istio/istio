package papertrail

import (
	"fmt"
	"net"
	"strings"

	"istio.io/istio/mixer/pkg/adapter"
)

type LoggerImpl struct{}

func (l *LoggerImpl) VerbosityLevel(level adapter.VerbosityLevel) bool {
	return true
}
func (l *LoggerImpl) Infof(format string, args ...interface{}) {
	fmt.Printf("INFO: "+format+"\n", args...)
}
func (l *LoggerImpl) Warningf(format string, args ...interface{}) {
	fmt.Printf("WARN: "+format+"\n", args...)
}
func (l *LoggerImpl) Errorf(format string, args ...interface{}) error {
	err := fmt.Errorf("Error: "+format+"\n", args...)
	fmt.Printf("%v", err)
	return err
}

func RunUDPServer(port int, logger adapter.Logger, stopChan chan struct{}, trackChan chan struct{}) {
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		logger.Errorf("Error: %v", err)
		return
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		logger.Errorf("Error: %v", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	go func() {
		for {
			noOfBytes, remoteAddr, err := conn.ReadFromUDP(buf)
			logger.Infof("udp server - data received: %s from %v", strings.TrimSpace(string(buf[0:noOfBytes])), remoteAddr)
			if err != nil {
				logger.Errorf("Error: %v", err)
				return
			}
			trackChan <- struct{}{}
		}
	}()
	var tobrk bool
	for {
		select {
		case <-stopChan:
			tobrk = true
		default:
		}
		if tobrk {
			break
		}
	}
}

package main

import (
	"istio.io/istio/mixer/test/prometheus"
	"os"
	"fmt"
)

func main() {
	addr := ""
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	s, err := prometheus.NewNoSessionServer(addr)
	if err != nil {
		fmt.Printf("unable to start sever: %v", err)
		os.Exit(-1)
	}

	s.Run()
	s.Wait()
}

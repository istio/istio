package main

import (
	"fmt"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func main() {
	dependencies.DoUnshare("", func() error {
		fmt.Println(file.MustAsString("/etc/nsswitch.conf"))
		return nil
	})
}
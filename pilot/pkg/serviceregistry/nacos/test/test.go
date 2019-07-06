package main

import (
	"fmt"
	"istio.io/istio/pilot/pkg/serviceregistry/nacos"
)

func main() {
	controller, err := nacos.NewController("127.0.0.1:8848")
	if err != nil {
		fmt.Println(err)
	}
	controller.Services()
}

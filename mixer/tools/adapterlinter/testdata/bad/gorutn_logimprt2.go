package testdata

import (
	"fmt"

	"github.com/golang/glog"
)

func M1() {
	glog.Infof("")
	go f()
	fmt.Println("M")
}

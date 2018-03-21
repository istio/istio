package testdata

import (
	"fmt"

	"github.com/golang/glog"
)

func f1() {
	//var v int
}

func M1() {
	glog.Infof("")
	go f() //nolint:adapterlinter
	fmt.Println("M")
}

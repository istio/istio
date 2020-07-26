package testdata

import (
	"fmt"
	"log"
)

func f() {
	//var v int
}

func M() {
	log.Println("")
	go f()
	fmt.Println("M")
}

package main

import (
	"fmt"

	"github.com/technosophos/moniker"
)

func main() {
	n := moniker.New()
	fmt.Printf("Your name is %q\n", n.Name())
}

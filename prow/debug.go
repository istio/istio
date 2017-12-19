package main

import (
	"fmt"
	"os/user"
)

func main() {
	for i := 0; i < 100; i++ {
		usr, err := user.Current()
		if err != nil {
			fmt.Println(usr)
			fmt.Println(err)
		}
		fmt.Printf("%d time: %s\n", i, usr.HomeDir)
	}
}

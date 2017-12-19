package main

import (
	"fmt"
	"os/user"
	"os"
)

func main() {
	usr, err := user.Current()
	if err != nil {
		fmt.Println(usr)
		fmt.Println(err)
	}
	fmt.Println(usr.HomeDir)
	fmt.Println(os.Getenv("USER"))
	fmt.Println(os.Getenv("HOME"))
}

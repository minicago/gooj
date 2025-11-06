package main

import (
	"fmt"

	"github.com/minicago/gooj/server"
)

func main() {
	// fmt.Println("Hello, World!")
	shutdownFlag := make(chan int)
	go func(ch chan int) {
		for {
			var x string
			fmt.Scan(&x)
			if x == "end" {
				ch <- 0
				break
			}
		}
	}(shutdownFlag)
	server.Listen(shutdownFlag)
}

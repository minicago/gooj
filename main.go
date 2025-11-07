package main

import (
	"flag"

	"github.com/minicago/gooj/cmd"
	"github.com/minicago/gooj/server"
)

func main() {
	// fmt.Println("Hello, World!")

	var method string
	var background bool
	flag.StringVar(&method, "method", "None", "run | cmd")
	flag.BoolVar(&background, "background", false, "--background = true | false")
	flag.Parse()

	switch method {
	case "run":
		server.StartServer(background)
	case "cmd":
		cmd.StartCmdConsole()
	}
}

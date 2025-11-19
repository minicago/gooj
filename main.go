package main

import (
	"flag"

	"github.com/minicago/gooj/cmd"
	"github.com/minicago/gooj/file_service"
	"github.com/minicago/gooj/judge"
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
		// start file service and judge goroutine before starting server
		file_service.StartDefault()
		judge.StartJudge()
		server.StartServer(background)
	case "cmd":
		cmd.StartCmdConsole()
	}
}

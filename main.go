package main

import (
	"flag"

	"github.com/minicago/gooj/cmd"
	"github.com/minicago/gooj/judge"
	"github.com/minicago/gooj/server"
	"github.com/minicago/gooj/sql_service"
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
		// initialize sqlite DB (data/app.db)
		if err := sql_service.Init("data/app.db"); err != nil {
			panic(err)
		}
		judge.StartJudge()
		server.StartServer(background)
	case "cmd":
		cmd.StartCmdConsole()
	}
}

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

	// // Initialize the SQLite database
	// if err := sql_service.Init("data/app.db"); err != nil {
	// 	panic("Failed to initialize database: " + err.Error())
	// }

	switch method {
	case "run":
		// start file service and judge goroutine before starting server
		// initialize sqlite DB (data/app.db)

		server.StartServer(background)
	case "cmd":
		cmd.StartCmdConsole()
	}
}

package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/minicago/gooj/cmd"
	"github.com/minicago/gooj/config"
	"github.com/minicago/gooj/judge"
	"github.com/minicago/gooj/server"
	"github.com/minicago/gooj/sql_service"
)

func main() {
	// fmt.Println("Hello, World!")

	var method string
	var background bool
	var configPath string
	var coordinatorAddr string
	var workerID string
	var workerConcurrency int
	flag.StringVar(&method, "method", "None", "run | cmd | judge-coordinator | judge-worker")
	flag.BoolVar(&background, "background", false, "--background = true | false")
	flag.StringVar(&configPath, "config", "config/config.yaml", "path to config file")
	flag.StringVar(&coordinatorAddr, "coordinator", "", "coordinator HTTP address (worker mode)")
	flag.StringVar(&workerID, "worker-id", "", "explicit worker ID (worker mode)")
	flag.IntVar(&workerConcurrency, "concurrency", 0, "worker concurrency (worker mode)")
	flag.Parse()

	// Load configuration
	if err := config.Load(configPath); err != nil {
		fmt.Printf("Warning: failed to load config: %v, using defaults\n", err)
	} else {
		fmt.Printf("Configuration loaded from %s\n", configPath)
	}

	switch method {
	case "run":
		// start file service and judge goroutine before starting server
		// initialize sqlite DB (data/app.db)
		server.StartServer(background)
	case "cmd":
		cmd.StartCmdConsole()
	case "judge-coordinator":
		// Standalone coordinator: owns the queue and distributes work to workers.
		// It does NOT need the web module.
		if err := sql_service.Init(); err != nil {
			log.Fatalf("failed to init database: %v", err)
		}
		judge.StartCoordinator()
	case "judge-worker":
		// Standalone judge worker: pulls tasks from a coordinator and judges them.
		addr := coordinatorAddr
		if addr == "" {
			addr = config.GetCoordinatorAddr()
		}
		conc := workerConcurrency
		if conc == 0 {
			conc = config.GetWorkerConcurrency()
		}
		judge.StartWorker(addr, workerID, conc)
	}
}

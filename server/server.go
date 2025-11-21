package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/minicago/gooj/cmd"
	"github.com/minicago/gooj/file_service"
	"github.com/minicago/gooj/judge"
	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
	"github.com/minicago/gooj/web"
	"github.com/sevlyar/go-daemon"
)

func listen(cmdChan chan string) {
	handler := web.NewRouter()

	srv := http.Server{
		Addr:    ":80",
		Handler: handler,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("%v", err)
		}
	}()
	fmt.Println("listening on :80")

	for {
		cmdStr := <-cmdChan
		cmdStr = strings.TrimSpace(cmdStr)
		if strings.EqualFold(cmdStr, "shutdown") {
			break
		}
		if strings.EqualFold(cmdStr, "clear message") {
			if err := web.ClearMessages(); err != nil {
				log.Printf("clear messages failed: %v", err)
			} else {
				log.Printf("messages cleared")
			}
		}
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("%v", err)
	}
}

func StartServer(isbackground bool) {

	if isbackground {
		cntxt := &daemon.Context{
			WorkDir: "./",
		}
		d, err := cntxt.Reborn()
		if err != nil {
			log.Fatal(err)
		}
		if d != nil {
			return
		}
	}
	if err := sql_service.Init("data/app.db"); err != nil {
		panic(err)
	}
	file_service.StartDefault()
	judge.StartJudge()
	manage.Init()
	cmdChan := make(chan string)
	shutdownChan := make(chan int)
	go cmd.StartCmdServer(cmdChan, shutdownChan)

	listen(cmdChan)
	shutdownChan <- 0
}

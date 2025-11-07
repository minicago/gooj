package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/minicago/gooj/cmd"
	"github.com/sevlyar/go-daemon"
)

func listen(cmdChan chan string) {
	mx := http.NewServeMux()
	mx.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to my website!")
	})

	fs := http.FileServer(http.Dir("static/"))
	mx.Handle("/static/", http.StripPrefix("/static/", fs))

	srv := http.Server{
		Addr:    ":80",
		Handler: mx,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("%v", err)
		}
	}()
	fmt.Println("listening")
	for {
		cmd := <-cmdChan
		if cmd == "shutdown" {
			break
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

	cmdChan := make(chan string)
	shutdownChan := make(chan int)
	go cmd.StartCmdServer(cmdChan, shutdownChan)

	listen(cmdChan)
	shutdownChan <- 0
}

package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
)

func Listen(shutdownFlag chan int) {
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
	<-shutdownFlag
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("%v", err)
	}

}

package web

import (
	"net/http"

	"github.com/gorilla/mux"
)

// NewRouter builds and returns the HTTP handler for the web endpoints
func NewRouter() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/message", MessageHandler).Methods("POST")
	// delete a message by its index (0-based)
	r.HandleFunc("/message/{index}", DeleteMessage).Methods("DELETE")
	r.HandleFunc("/board", BoardHandler).Methods("GET")

	// static files under /static/
	fs := http.FileServer(http.Dir("static/"))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	// root serves index.html
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	return r
}

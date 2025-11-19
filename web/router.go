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
	// submission page (GET serves static submit page, POST handled by SubmitHandler)
	r.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/submit.html")
	}).Methods("GET")
	r.HandleFunc("/postboard", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/postboard.html")
	}).Methods("GET")
	r.HandleFunc("/submit", SubmitHandler).Methods("POST")
	// problem page (serves static problem viewer)
	r.HandleFunc("/problem", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/problem.html")
	}).Methods("GET")
	r.HandleFunc("/problemlist", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/problemlist.html")
	}).Methods("GET")
	// API to fetch statement.md and config.json for a problem
	r.HandleFunc("/api/problem/{id}", ProblemDataHandler).Methods("GET")
	r.HandleFunc("/problems", ProblemsHandler).Methods("GET")
	r.HandleFunc("/register", RegisterHandler).Methods("POST")
	r.HandleFunc("/login", LoginHandler).Methods("POST")
	r.HandleFunc("/last_submission", LastSubmissionHandler).Methods("GET")
	r.HandleFunc("/result/{user}/{problem}", ResultHandler).Methods("GET")
	r.HandleFunc("/codefile/{user}/{problem}", CodeFileHandler).Methods("GET")

	// static files under /static/
	fs := http.FileServer(http.Dir("static/"))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	// root serves index.html
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	return r
}

package web

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/edit"
	"github.com/minicago/gooj/manage"
)

// contextKey is a private type used for storing values in request contexts
// to avoid collisions with other context keys across packages.

// NewRouter builds and returns the HTTP handler for the web endpoints

func NewRouter() http.Handler {
	r := mux.NewRouter()

	// global auth middleware: protect all routes except public ones
	r.Use(manage.AuthMiddleWare)

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
	r.HandleFunc("/manage", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/manage.html")
	}).Methods("GET")
	r.HandleFunc("/manage_users", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/manage_users.html")
	}).Methods("GET")

	r.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/register.html")
	}).Methods("GET")

	r.HandleFunc("/create_user", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/create_user.html")
	}).Methods("GET")

	r.HandleFunc("/create_group", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/create_group.html")
	}).Methods("GET")
	r.HandleFunc("/upload_problem", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/upload_problem.html")
	}).Methods("GET")
	r.HandleFunc("/edit", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/edit.html")
	}).Methods("GET")
	// API to fetch statement.md and config.json for a problem
	r.HandleFunc("/api/problem/{id}", ProblemDataHandler).Methods("GET")
	r.HandleFunc("/problems", ProblemsHandler).Methods("GET")
	r.HandleFunc("/register", RegisterHandler).Methods("POST")
	r.HandleFunc("/login", LoginHandler).Methods("POST")
	r.HandleFunc("/last_submission", LastSubmissionHandler).Methods("GET")
	r.HandleFunc("/result/{user}/{problem}", ResultHandler).Methods("GET")
	r.HandleFunc("/codefile/{user}/{problem}", CodeFileHandler).Methods("GET")
	// Added `/api/users` endpoint to list all users
	r.HandleFunc("/api/users", GetUsersHandler).Methods("GET")
	r.HandleFunc("/api/allUsers", GetAllUsersHandler).Methods("GET")
	r.HandleFunc("/api/groups", manage.ListGroupsHandler).Methods("GET")
	r.HandleFunc("/api/user_permissions", manage.GetUserPermissionsHandler).Methods("GET")
	// User management endpoints
	r.HandleFunc("/api/pending_users", GetPendingUsersHandler).Methods("GET")
	r.HandleFunc("/api/approved_users", GetApprovedUsersHandler).Methods("GET")
	r.HandleFunc("/api/approve/{username}", ApproveUserHandler).Methods("POST")
	r.HandleFunc("/api/reject/{username}", RejectUserHandler).Methods("POST")

	// API to create a user (admin)
	r.HandleFunc("/api/create_user", manage.CreateUserHandler).Methods("POST")
	r.HandleFunc("/api/create_group", manage.CreateGroupHandler).Methods("POST")
	r.HandleFunc("/api/update_group_creator", manage.UpdateGroupCreatorHandler).Methods("POST")
	r.HandleFunc("/api/delete_group", manage.DeleteGroupHandler).Methods("POST")
	r.HandleFunc("/api/reset_password", manage.ResetPasswordHandler).Methods("POST")
	r.HandleFunc("/api/delete_user", manage.DeleteUserHandler).Methods("POST")
	r.HandleFunc("/api/delete_problem", manage.DeleteProblemHandler).Methods("POST")

	// Edit endpoints for modifying statements and adding test data
	r.HandleFunc("/edit/modify", edit.ModifyProblemStatementHandler).Methods("POST")
	r.HandleFunc("/edit/add_test", edit.AddTestDataHandler).Methods("POST")
	// Import tuack package from zip file
	r.HandleFunc("/api/import_tuack", edit.ImportTuackHandler).Methods("POST")

	// Upload problem endpoint
	r.HandleFunc("/api/upload_problem", UploadProblemHandler).Methods("POST")

	// Submission endpoints
	r.HandleFunc("/submissions", SubmissionsHandler).Methods("GET")
	r.HandleFunc("/submission/{id}", SubmissionDetailHandler).Methods("GET")
	r.HandleFunc("/api/submissions", GetSubmissionsHandler).Methods("GET")
	r.HandleFunc("/api/submission/{id}", GetSubmissionHandler).Methods("GET")
	r.HandleFunc("/api/problem_stats", GetProblemStatsHandler).Methods("GET")

	// static files under /static/
	fs := http.FileServer(http.Dir("static/"))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	// root serves index.html
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	return r
}

package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
)

// CodeFileHandler returns last submitted code and result for a user/problem.
//
// Visibility rules:
//   - The owner of the code can always view it.
//   - Editors (teachers/admins) can always view it.
//   - During an *ongoing* contest the problem belongs to, only the owner (and
//     editors) may view the code; everyone else gets 403. This keeps contest
//     solutions private while the contest is running.
//   - After the contest ends (or for non-contest problems) the code is public.
//
// Evaluation details (test results/scores) are additionally gated by the
// problem's TestVisible flag: non-editors only see them once the problem is
// revealed.
func CodeFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := vars["user"]
	problem := vars["problem"]
	problemID, err := parseProblemID(problem)
	if err != nil {
		http.Error(w, "invalid problem id", http.StatusBadRequest)
		return
	}

	currentUsername := manage.CurrentUsername(r)
	db := sql_service.DB()

	isOwner := currentUsername == user
	isEditor := manage.CheckUserPermission(currentUsername, "EditPermission")

	// Contest gate: block non-owners/non-editors from viewing code during a contest.
	inOngoing, cerr := sql_service.IsProblemInOngoingContest(problemID)
	if cerr == nil && inOngoing && !isOwner && !isEditor {
		http.Error(w, "forbidden during contest", http.StatusForbidden)
		return
	}

	// fetch last submission from DB
	sub, results, err := sql_service.GetLastSubmission(user, strconv.FormatUint(uint64(problemID), 10))
	if err != nil {
		http.Error(w, "no submission", http.StatusNotFound)
		return
	}

	// Evaluation details are only returned when the problem is revealed or the
	// viewer is an editor. The raw code is always returned when visible.
	canViewTests := isEditor
	var problemRecord sql_service.Problem
	if db.First(&problemRecord, problemID).Error == nil {
		canViewTests = canViewTests || problemRecord.TestVisible
	}
	summary := map[string]interface{}{"status": sub.Status}
	if canViewTests {
		summary["test_results"] = results
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": sub.Code, "summary": summary})
}

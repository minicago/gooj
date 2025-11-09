package web

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// DeleteMessage removes the message at the given 0-based index from storage
func DeleteMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idxStr := vars["index"]
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, "invalid index", http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile("data/message.txt")
	msgs := []string{}
	if err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l != "" {
				msgs = append(msgs, l)
			}
		}
	}

	if idx < 0 || idx >= len(msgs) {
		http.Error(w, "index out of range", http.StatusBadRequest)
		return
	}

	// remove element
	msgs = append(msgs[:idx], msgs[idx+1:]...)

	var out string
	if len(msgs) > 0 {
		out = strings.Join(msgs, "\n") + "\n"
	} else {
		out = ""
	}

	if err := os.WriteFile("data/message.txt", []byte(out), 0644); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("write file failed: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

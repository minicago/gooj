package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// BoardHandler returns the saved messages as JSON
func BoardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msgs []string
	data, err := os.ReadFile("data/message.txt")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				msgs = append(msgs, l)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": msgs})
}

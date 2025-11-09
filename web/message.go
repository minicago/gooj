package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type messagePayload struct {
	Message string `json:"message"`
}

// MessageHandler accepts POST requests to save a message into data/message.txt
func MessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg string
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var p messagePayload
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&p); err != nil && err != io.EOF {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		msg = p.Message
	} else {
		if err := r.ParseForm(); err == nil {
			msg = r.FormValue("message")
		}
		if msg == "" {
			b, _ := io.ReadAll(r.Body)
			msg = string(b)
		}
	}

	msg = strings.TrimSpace(msg)
	if msg == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll("data", 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("mkdir data failed: %v", err)
		return
	}

	f, err := os.OpenFile("data/message.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("open file failed: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(msg + "\n"); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		log.Printf("write file failed: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ClearMessages truncates the message file (clears all saved messages)
func ClearMessages() error {
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}
	return os.WriteFile("data/message.txt", []byte(""), 0644)
}

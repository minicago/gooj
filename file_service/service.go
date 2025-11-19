package file_service

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
)

// Service serializes access to files by path. Each path has a dedicated goroutine
// that performs read/write/modify requests to ensure process-level safety.
type Service struct {
	mu     sync.Mutex
	chans  map[string]chan request
	closed bool
}

type request struct {
	kind   string // "read","write","modify"
	data   []byte
	modify func([]byte) ([]byte, error)
	resp   chan response
}

type response struct {
	data []byte
	err  error
}

var defaultSvc *Service

// StartDefault initializes the default file service (singleton).
func StartDefault() *Service {
	if defaultSvc != nil {
		return defaultSvc
	}
	s := &Service{chans: make(map[string]chan request)}
	defaultSvc = s
	return s
}

// Default returns the global service. StartDefault must be called first.
func Default() *Service {
	return defaultSvc
}

func (s *Service) ensure(path string) chan request {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.chans[path]
	if ok {
		return ch
	}
	ch = make(chan request)
	s.chans[path] = ch
	// spawn worker for this file path
	go s.worker(path, ch)
	return ch
}

func (s *Service) worker(path string, ch chan request) {
	// ensure parent dir exists
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	for req := range ch {
		switch req.kind {
		case "read":
			data, err := ioutil.ReadFile(path)
			if os.IsNotExist(err) {
				data = []byte{}
				err = nil
			}
			req.resp <- response{data: data, err: err}
		case "write":
			err := ioutil.WriteFile(path, req.data, 0644)
			req.resp <- response{data: nil, err: err}
		case "modify":
			cur, err := ioutil.ReadFile(path)
			if os.IsNotExist(err) {
				cur = []byte{}
				err = nil
			}
			if err != nil {
				req.resp <- response{data: nil, err: err}
				continue
			}
			out, err := req.modify(cur)
			if err != nil {
				req.resp <- response{data: nil, err: err}
				continue
			}
			if err := ioutil.WriteFile(path, out, 0644); err != nil {
				req.resp <- response{data: nil, err: err}
				continue
			}
			req.resp <- response{data: out, err: nil}
		}
	}
}

// ReadFile reads a file via the file service
func (s *Service) ReadFile(path string) ([]byte, error) {
	ch := s.ensure(path)
	respCh := make(chan response)
	ch <- request{kind: "read", resp: respCh}
	resp := <-respCh
	return resp.data, resp.err
}

// WriteFile writes data to file via the file service
func (s *Service) WriteFile(path string, data []byte) error {
	ch := s.ensure(path)
	respCh := make(chan response)
	ch <- request{kind: "write", data: data, resp: respCh}
	resp := <-respCh
	return resp.err
}

// ModifyFile runs modify function with current file content and writes the returned bytes
func (s *Service) ModifyFile(path string, modify func([]byte) ([]byte, error)) ([]byte, error) {
	ch := s.ensure(path)
	respCh := make(chan response)
	ch <- request{kind: "modify", modify: modify, resp: respCh}
	resp := <-respCh
	return resp.data, resp.err
}

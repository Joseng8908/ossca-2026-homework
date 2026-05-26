//go:build linux

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type unshareNetnsRequest struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type unshareNetnsResponse struct {
	ParentPID int `json:"parent_pid"`
	ChildPID  int `json:"child_pid"`
}

func handleUnshareNetns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req unshareNetnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	if req.Path == "" || !filepath.IsAbs(req.Path) {
		http.Error(w, "path must be an absolute path", http.StatusBadRequest)
		return
	}

	cmd := exec.Command(req.Path, req.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Unshareflags: syscall.CLONE_NEWNET,
	}

	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go func() {
		_ = cmd.Wait()
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(unshareNetnsResponse{
		ParentPID: os.Getpid(),
		ChildPID:  cmd.Process.Pid,
	})
}

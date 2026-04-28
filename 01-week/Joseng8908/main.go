package main 

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"syscall"
)

// RequestBody
type RequestBody struct {
	Path string `json:"path"`
	Args []string `json:"args"`
}

// ResponseBody: pid, child id
type ResponseBody struct {
	ParentPid int `json:"parent_pid"`
	ChildPid int `json:"child_pid"`
}

func handleNetNs(w http.ResponseWriter, r *http.Request) {
	// method check - post
	if r.Method != http.MethodPost {
		http.Error(w, "Method is Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// JSON request parsing
	var req RequestBody
	json.NewDecoder(r.Body).Decode(&req)

	// making child process 
	cmd := exec.Command(req.Path, req.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// setting flag 
		Unshareflags: syscall.CLONE_NEWNET,
	}

	cmd.Start()

	// parent wait - goroutine
	go func() {
		cmd.Wait()
	}()



	// response formatting
	res := ResponseBody{
		ParentPid: os.Getpid(),
		ChildPid: cmd.Process.Pid,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}

func main() {
	http.HandleFunc("/unshare/netns", handleNetNs)

	fmt.Println("Server 실행중")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server Error: %v", err)
	}
}

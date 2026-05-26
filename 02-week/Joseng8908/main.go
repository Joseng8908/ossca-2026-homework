//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"ossca-2026-homework/02-week/Joseng/internal/netns"
	"ossca-2026-homework/02-week/Joseng/internal/veth"

	"golang.org/x/sys/unix"
)

type createNetnsReq struct {
	Name string `json:"name"`
}

type createNetnsResp struct {
	Name      string `json:"name"`
	NetnsPath string `json:"netns_path"`
}

type createVethReq struct {
	HostIfname string `json:"host_ifname"`
	PeerIfname string `json:"peer_ifname"`
	HostIP     string `json:"host_ip"`
	PeerIP     string `json:"peer_ip"`
}

type execReq struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type execResp struct {
	Name      string `json:"name"`
	ParentPID int    `json:"parent_pid"`
	ChildPID  int    `json:"child_pid"`
}

func main() {
	mgr, err := netns.NewNetnsManager()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /netns", func(w http.ResponseWriter, r *http.Request) {
		var req createNetnsReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entry, err := mgr.Create(req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createNetnsResp{
			Name:      entry.Name,
			NetnsPath: entry.MountPath,
		})
	})

	mux.HandleFunc("POST /netns/{name}/veth", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		var req createVethReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := veth.Veth(name, &veth.Config{
			HostIfname: req.HostIfname,
			PeerIfname: req.PeerIfname,
			HostIP:     req.HostIP,
			PeerIP:     req.PeerIP,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("POST /netns/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		var req execReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		parentPID, childPID, err := execInNetns("/var/run/netns/"+name, req.Path, req.Args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(execResp{
			Name:      name,
			ParentPID: parentPID,
			ChildPID:  childPID,
		})
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("server listening on :8080")

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func execInNetns(nsPath, path string, args []string) (int, int, error) {
	var filtered []string
	for _, a := range args {
		if a != "" {
			filtered = append(filtered, a)
		}
	}

	type result struct {
		childPID int
		err      error
	}
	ch := make(chan result, 1)

	go func() {
		// Setns는 thread-local 연산이므로 OS thread 고정 필수.
		runtime.LockOSThread()

		origNsFd, err := unix.Open("/proc/self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			runtime.UnlockOSThread()
			ch <- result{0, fmt.Errorf("open host ns: %w", err)}
			return
		}
		defer unix.Close(origNsFd)

		nsFd, err := unix.Open(nsPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			runtime.UnlockOSThread()
			ch <- result{0, fmt.Errorf("open target ns %s: %w", nsPath, err)}
			return
		}
		defer unix.Close(nsFd)

		// 이 thread를 named namespace로 진입
		if err := unix.Setns(nsFd, unix.CLONE_NEWNET); err != nil {
			runtime.UnlockOSThread()
			ch <- result{0, fmt.Errorf("setns %s: %w", nsPath, err)}
			return
		}

		// fork+exec: 자식 프로세스는 현재 thread의 namespace(= named ns) 상속
		cmd := exec.Command(path, filtered...)
		if err := cmd.Start(); err != nil {
			_ = unix.Setns(origNsFd, unix.CLONE_NEWNET)
			runtime.UnlockOSThread()
			ch <- result{0, fmt.Errorf("start process: %w", err)}
			return
		}

		pid := cmd.Process.Pid
		go func() { _ = cmd.Wait() }()

		// host ns로 복귀
		if err := unix.Setns(origNsFd, unix.CLONE_NEWNET); err != nil {
			// 복귀 실패: 오염된 thread를 pool에 반환하면 안 됨
			ch <- result{pid, fmt.Errorf("restore host ns: %w", err)}
			runtime.Goexit()
			return
		}

		runtime.UnlockOSThread()
		ch <- result{pid, nil}
	}()

	res := <-ch
	if res.err != nil {
		return 0, 0, res.err
	}
	return os.Getpid(), res.childPID, nil
}

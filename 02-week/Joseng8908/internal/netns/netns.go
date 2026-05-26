package netns

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func (m *NetnsManager) Create(name string) (NetnsEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.store[name]; exists {
		return NetnsEntry{}, fmt.Errorf("netns %s already exists", name)
	}

	mountPath := m.MountPath(name)
	errCh := make(chan error, 1)

	go func() {
		// 이 goroutine의 OS thread 고정
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// 현재(host) ns 저장
		origNs, err := os.Open("/proc/self/ns/net")
		if err != nil {
			errCh <- err
			return
		}
		defer origNs.Close()

		// 새 netns로 진입
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			errCh <- err
			return
		}

		// bind mount로 고정 (프로세스 없어도 ns 유지)
		os.WriteFile(mountPath, []byte{}, 0444)
		if err := unix.Mount("/proc/self/ns/net", mountPath, "bind", unix.MS_BIND, ""); err != nil {
			os.Remove(mountPath)
			errCh <- err
			return
		}

		// host ns로 복귀
		if err := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); err != nil {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	if err := <-errCh; err != nil {
		return NetnsEntry{}, err
	}

	entry := NetnsEntry{
		Name:      name,
		MountPath: mountPath,
	}
	m.store[name] = entry
	return entry, nil
}

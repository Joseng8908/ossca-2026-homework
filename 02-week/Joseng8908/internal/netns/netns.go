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
		return NetnsEntry{}, fmt.Errorf("netns %q already exists", name)
	}

	mountPath := m.MountPath(name)

	if err := createNamedNetns(mountPath); err != nil {
		return NetnsEntry{}, err
	}

	entry := NetnsEntry{
		Name:      name,
		MountPath: mountPath,
	}
	m.store[name] = entry
	return entry, nil
}

// createNamedNetns: 새 network namespace를 만들고 mountPath에 bind mount로 고정한다.
// goroutine 하나를 전용 OS thread에 고정해서 unshare/setns 작업을 격리한다.
// Go runtime이 다른 goroutine을 이 thread에 스케줄하지 않도록 LockOSThread 필수.
func createNamedNetns(mountPath string) error {
	type result struct{ err error }
	ch := make(chan result, 1)

	go func() {
		// 이 goroutine을 OS thread에 고정.
		// unshare는 thread-local 연산이므로 goroutine이 다른 thread로 이동하면 안 된다.
		runtime.LockOSThread()
		// 주의: Setns 복귀에 실패하면 이 thread는 오염된 상태다.
		// defer UnlockOSThread는 오염된 thread를 pool에 돌려줄 수 있다.
		// 실패 경로에서 명시적으로 runtime.Goexit()을 호출해 thread를 소멸시킨다.
		defer runtime.UnlockOSThread()

		// Unshare 전에 host ns fd 저장.
		// O_CLOEXEC: 자식 프로세스에 fd 누수 방지.
		origNsFd, err := unix.Open("/proc/self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			ch <- result{fmt.Errorf("open host ns fd: %w", err)}
			return
		}
		defer unix.Close(origNsFd)

		// 이 OS thread만 새 network namespace로 분리.
		// 다른 thread(= host ns)는 영향 없음.
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			ch <- result{fmt.Errorf("unshare CLONE_NEWNET: %w", err)}
			return
		}

		// bind mount 대상 파일 생성 (mount point는 파일이어야 한다).
		if err := os.WriteFile(mountPath, []byte{}, 0444); err != nil {
			restoreOrExit(origNsFd)
			ch <- result{fmt.Errorf("create mount point %s: %w", mountPath, err)}
			return
		}

		// /proc/thread-self/ns/net: 현재 thread의 ns를 가리키는 경로.
		// /proc/self/ns/net은 tgid(메인 스레드) 기준이라 host ns를 가리킨다 — 쓰면 안 된다.
		if err := unix.Mount("/proc/thread-self/ns/net", mountPath, "bind", unix.MS_BIND, ""); err != nil {
			_ = os.Remove(mountPath)
			restoreOrExit(origNsFd)
			ch <- result{fmt.Errorf("bind mount to %s: %w", mountPath, err)}
			return
		}

		// host ns로 복귀.
		if err := unix.Setns(origNsFd, unix.CLONE_NEWNET); err != nil {
			// 복귀 실패: thread가 새 ns에 남아있는 오염 상태.
			// UnlockOSThread 후 pool에 돌아가면 다른 goroutine에 영향을 준다.
			// Goexit으로 이 goroutine(= thread)을 종료해 오염 전파를 막는다.
			ch <- result{fmt.Errorf("restore host ns: %w", err)}
			runtime.Goexit()
			return
		}

		ch <- result{}
	}()

	return (<-ch).err
}

// restoreOrExit: host ns로 복귀를 시도하고, 실패하면 thread를 소멸시킨다.
func restoreOrExit(origNsFd int) {
	if err := unix.Setns(origNsFd, unix.CLONE_NEWNET); err != nil {
		runtime.Goexit()
	}
}

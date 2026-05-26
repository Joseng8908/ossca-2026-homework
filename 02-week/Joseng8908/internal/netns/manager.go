package netns

import (
	"fmt"
	"os"
	"sync"
)

type NetnsEntry struct {
	Name string
	MountPath string
}

type NetnsManager struct {
	mu sync.Mutex
	store map[string]NetnsEntry
	basePath string
}

func NewNetnsManager() (*NetnsManager, error) {
	if err := os.MkdirAll("/var/run/netns", 0755); err != nil {
		return nil, err
	}
	return &NetnsManager{
		store: make(map[string]NetnsEntry),
		basePath: "/var/run/netns",
	}, nil
}

func (m *NetnsManager) MountPath(name string) string {
	return fmt.Sprintf("%s/%s", m.basePath, name)
}

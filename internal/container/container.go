package container

import (
	"context"
	"io"
)

// Manager handles container lifecycle operations.
type Manager struct {
	defaultImage   string
	defaultCPU     string
	defaultMem     string
	networkMode    string
	timeoutMinutes int
}

// ContainerConfig holds configuration for creating a new container.
type ContainerConfig struct {
	Image       string
	CPULimit    string
	MemLimit    string
	NetworkMode string
	WorkDir     string
	Env         map[string]string
	Mounts      []Mount
}

// Mount represents a bind mount into a container.
type Mount struct {
	Source   string
	Target  string
	ReadOnly bool
}

// ContainerInfo holds runtime information about a container.
type ContainerInfo struct {
	ID     string
	Status string
	Image  string
}

// ExecResult holds the result of executing a command in a container.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// New creates a new container Manager.
func New(image, cpu, mem, network string, timeout int) *Manager {
	return &Manager{
		defaultImage:   image,
		defaultCPU:     cpu,
		defaultMem:     mem,
		networkMode:    network,
		timeoutMinutes: timeout,
	}
}

// Create creates a new container with the given configuration.
func (m *Manager) Create(ctx context.Context, cfg *ContainerConfig) (string, error) {
	return "", nil
}

// Start starts a container by ID.
func (m *Manager) Start(ctx context.Context, containerID string) error {
	return nil
}

// Stop stops a running container.
func (m *Manager) Stop(ctx context.Context, containerID string) error {
	return nil
}

// Remove removes a container by ID.
func (m *Manager) Remove(ctx context.Context, containerID string) error {
	return nil
}

// Exec executes a command inside a running container.
func (m *Manager) Exec(ctx context.Context, containerID string, cmd []string) (*ExecResult, error) {
	return nil, nil
}

// CopyTo copies a file or directory into a container.
func (m *Manager) CopyTo(ctx context.Context, containerID, srcPath, dstPath string) error {
	return nil
}

// CopyFrom copies a file or directory out of a container.
func (m *Manager) CopyFrom(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error) {
	return nil, nil
}

// List returns all containers managed by axiom.
func (m *Manager) List(ctx context.Context) ([]*ContainerInfo, error) {
	return nil, nil
}

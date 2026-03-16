// Package tunnel implements Cloudflare Tunnel integration for exposing
// the Axiom API server to remote Claw orchestrators.
//
// See Architecture.md Section 24.4.
package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Manager handles Cloudflare Tunnel lifecycle.
type Manager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	publicURL string
	apiPort   int
}

// NewManager creates a tunnel Manager targeting the given API port.
func NewManager(apiPort int) *Manager {
	return &Manager{apiPort: apiPort}
}

// Start launches a Cloudflare Tunnel pointing to the local API server.
// Returns the public URL for remote Claw connections.
// See Architecture Section 24.4.
func (m *Manager) Start() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return m.publicURL, nil
	}

	// Launch cloudflared with a quick tunnel.
	m.cmd = exec.Command("cloudflared", "tunnel", "--url",
		fmt.Sprintf("http://localhost:%d", m.apiPort))
	m.cmd.Stderr = os.Stderr

	stdout, err := m.cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := m.cmd.Start(); err != nil {
		return "", fmt.Errorf("start cloudflared: %w (is cloudflared installed?)", err)
	}

	// Parse the public URL from cloudflared output.
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "trycloudflare.com") || strings.Contains(line, "https://") {
			// Extract the URL.
			parts := strings.Fields(line)
			for _, p := range parts {
				if strings.HasPrefix(p, "https://") {
					m.publicURL = p
					m.running = true
					return m.publicURL, nil
				}
			}
		}
	}

	m.running = true
	m.publicURL = fmt.Sprintf("https://<pending>.trycloudflare.com")
	return m.publicURL, nil
}

// Stop shuts down the Cloudflare Tunnel.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Signal(os.Interrupt)
		m.cmd.Wait()
	}

	m.running = false
	m.publicURL = ""
	return nil
}

// IsRunning returns true if the tunnel is active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// PublicURL returns the tunnel's public URL.
func (m *Manager) PublicURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publicURL
}

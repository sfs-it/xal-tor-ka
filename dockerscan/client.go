// Package dockerscan queries a read-only docker-socket-proxy to discover running
// containers and their published ports, so the admin UI can propose ready-made
// vhosts. It never touches the raw docker socket (that lives only in the
// socket-proxy sidecar). See BLUEPRINT.md §9 (admin) — discovery helper.
package dockerscan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Port is a container port mapping as reported by the Docker API.
type Port struct {
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// Container is a discovered running container.
type Container struct {
	Name  string
	Ports []Port
}

type apiContainer struct {
	Names []string `json:"Names"`
	State string   `json:"State"`
	Ports []Port   `json:"Ports"`
}

// List returns the running containers reported by the docker-socket-proxy at
// baseURL (e.g. "http://docker-socket-proxy:2375").
func List(ctx context.Context, baseURL string) ([]Container, error) {
	url := strings.TrimRight(baseURL, "/") + "/containers/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker api: HTTP %d", resp.StatusCode)
	}
	var raw []apiContainer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode docker api: %w", err)
	}
	out := make([]Container, 0, len(raw))
	for _, c := range raw {
		if c.State != "running" {
			continue
		}
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, Container{Name: name, Ports: c.Ports})
	}
	return out, nil
}

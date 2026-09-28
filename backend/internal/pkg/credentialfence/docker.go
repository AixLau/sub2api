// Package credentialfence provides the offline cutover fence for a single Docker
// Compose host. It is not a scheduler and never authorizes provider identities.
package credentialfence

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
)

type Docker struct{ Project, Service string }
type Evidence struct {
	IDs     []string `json:"container_ids"`
	Project string   `json:"project"`
	Service string   `json:"service"`
}
type state struct {
	ID         string
	State      struct{ Running, Restarting, Paused bool }
	HostConfig struct {
		NetworkMode   string
		Privileged    bool
		RestartPolicy struct{ Name string }
	}
	NetworkSettings struct{ Networks map[string]json.RawMessage }
	Config          struct{ Labels map[string]string }
}

func docker(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, errors.New("Docker fencing command failed")
	}
	return out, nil
}
func (d Docker) inventory(ctx context.Context) ([]state, error) {
	if strings.TrimSpace(d.Project) == "" || strings.TrimSpace(d.Service) == "" {
		return nil, errors.New("exact Compose project and gateway service required")
	}
	out, err := docker(ctx, "ps", "--all", "--quiet", "--no-trunc", "--filter", "label=com.docker.compose.project="+d.Project, "--filter", "label=com.docker.compose.service="+d.Service)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil, errors.New("no gateway containers found; refusing empty fence")
	}
	args := append([]string{"inspect"}, ids...)
	out, err = docker(ctx, args...)
	if err != nil {
		return nil, err
	}
	var states []state
	err = json.Unmarshal(out, &states)
	return states, err
}
func (d Docker) Fence(ctx context.Context) (Evidence, error) {
	states, err := d.inventory(ctx)
	if err != nil {
		return Evidence{}, err
	}
	e := Evidence{Project: d.Project, Service: d.Service}
	// Validate the entire inventory before changing any container.
	for _, s := range states {
		if s.HostConfig.Privileged || s.HostConfig.NetworkMode == "host" || strings.HasPrefix(s.HostConfig.NetworkMode, "container:") {
			return e, errors.New("unsupported gateway network/privilege topology")
		}
		e.IDs = append(e.IDs, s.ID)
	}
	sort.Strings(e.IDs)
	for _, s := range states {
		if _, err = docker(ctx, "update", "--restart=no", s.ID); err != nil {
			return e, err
		}
		if _, err = docker(ctx, "stop", "--time", "30", s.ID); err != nil {
			return e, err
		}
		for network := range s.NetworkSettings.Networks {
			if _, err = docker(ctx, "network", "disconnect", "--force", network, s.ID); err != nil {
				return e, err
			}
		}
	}
	return e, d.Verify(ctx, e)
}
func (d Docker) Verify(ctx context.Context, e Evidence) error {
	states, err := d.inventory(ctx)
	if err != nil {
		return err
	}
	var ids []string
	for _, s := range states {
		ids = append(ids, s.ID)
		if s.State.Running || s.State.Restarting || s.State.Paused || len(s.NetworkSettings.Networks) != 0 || s.HostConfig.RestartPolicy.Name != "no" {
			return errors.New("gateway fence lost")
		}
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != strings.Join(e.IDs, ",") || e.Project != d.Project || e.Service != d.Service {
		return errors.New("gateway inventory changed")
	}
	return nil
}

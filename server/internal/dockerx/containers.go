package dockerx

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/pkg/stdcopy"
)

// Container is the trimmed view the dashboard needs. The Engine returns a
// great deal more; sending all of it to the browser for a list of forty
// containers is wasteful and leaks environment detail nobody asked for.
type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	ImageID string `json:"image_id"`
	// State is the Engine vocabulary: created, running, paused, restarting,
	// removing, exited, dead.
	State  string `json:"state"`
	Status string `json:"status"`
	Health string `json:"health,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	Ports     []Port    `json:"ports"`
	Networks  []string  `json:"networks"`

	// ManagedBy names the tool that created this container, read from labels.
	// Containers the platform did not deploy are still listed and operable --
	// discovering what is already running is the point of connecting a server.
	ManagedBy string `json:"managed_by,omitempty"`
	// DeploymentID links back to a deployment when the platform owns it.
	DeploymentID   string `json:"deployment_id,omitempty"`
	ComposeProject string `json:"compose_project,omitempty"`
}

type Port struct {
	Host      string `json:"host,omitempty"`
	HostPort  uint16 `json:"host_port,omitempty"`
	Container uint16 `json:"container_port"`
	Protocol  string `json:"protocol"`
}

// Labels the platform writes on containers it creates.
const (
	LabelManagedBy    = "dev.dockdeploy.managed"
	LabelDeploymentID = "dev.dockdeploy.deployment"
)

// ListContainers returns every container, running or not.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	raw, err := c.api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("dockerx: list containers: %w", err)
	}

	out := make([]Container, 0, len(raw))
	for _, item := range raw {
		out = append(out, newContainer(item))
	}

	// Running first, then by name: the dashboard should open on what is live,
	// not on whatever the daemon happened to return first.
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].State == "running") != (out[j].State == "running") {
			return out[i].State == "running"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func newContainer(item container.Summary) Container {
	result := Container{
		ID:             item.ID,
		Image:          item.Image,
		ImageID:        item.ImageID,
		State:          item.State,
		Status:         item.Status,
		CreatedAt:      time.Unix(item.Created, 0).UTC(),
		ManagedBy:      item.Labels[LabelManagedBy],
		DeploymentID:   item.Labels[LabelDeploymentID],
		ComposeProject: item.Labels["com.docker.compose.project"],
		Ports:          make([]Port, 0, len(item.Ports)),
		Networks:       make([]string, 0, len(item.NetworkSettings.Networks)),
	}

	// The Engine reports names with a leading slash.
	if len(item.Names) > 0 {
		result.Name = strings.TrimPrefix(item.Names[0], "/")
	}

	// "Up 2 hours (healthy)" is the only place the summary carries health.
	if start := strings.Index(item.Status, "("); start >= 0 {
		if end := strings.Index(item.Status[start:], ")"); end > 0 {
			health := item.Status[start+1 : start+end]
			if strings.Contains(health, "healthy") || strings.Contains(health, "starting") {
				result.Health = health
			}
		}
	}

	// Docker reports a published port once per address family, so a container
	// bound on both IPv4 and IPv6 appears twice. Collapse them: the mapping is
	// what matters to a reader, not which stack it came from.
	seen := make(map[Port]bool, len(item.Ports))
	for _, port := range item.Ports {
		mapped := Port{
			HostPort:  port.PublicPort,
			Container: port.PrivatePort,
			Protocol:  port.Type,
		}
		// Keep the bind address only when it is meaningful. A service on
		// 127.0.0.1 is not publicly reachable and that is worth showing.
		if port.IP != "" && port.IP != "0.0.0.0" && port.IP != "::" {
			mapped.Host = port.IP
		}
		if seen[mapped] {
			continue
		}
		seen[mapped] = true
		result.Ports = append(result.Ports, mapped)
	}
	sort.Slice(result.Ports, func(i, j int) bool {
		if result.Ports[i].Container != result.Ports[j].Container {
			return result.Ports[i].Container < result.Ports[j].Container
		}
		return result.Ports[i].HostPort < result.Ports[j].HostPort
	})
	for name := range item.NetworkSettings.Networks {
		result.Networks = append(result.Networks, name)
	}
	sort.Strings(result.Networks)

	return result
}

// InspectContainer returns the full Engine detail for one container.
func (c *Client) InspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	details, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("dockerx: inspect %s: %w", short(id), err)
	}
	return details, nil
}

// Action is a container lifecycle operation.
type Action string

const (
	ActionStart   Action = "start"
	ActionStop    Action = "stop"
	ActionRestart Action = "restart"
	ActionKill    Action = "kill"
	ActionPause   Action = "pause"
	ActionUnpause Action = "unpause"
	ActionRemove  Action = "remove"
)

func (a Action) Valid() bool {
	switch a {
	case ActionStart, ActionStop, ActionRestart, ActionKill, ActionPause, ActionUnpause, ActionRemove:
		return true
	}
	return false
}

// stopTimeout gives a container a chance to shut down cleanly before SIGKILL.
var stopTimeout = 10

// Do performs a lifecycle action.
func (c *Client) Do(ctx context.Context, action Action, id string) error {
	var err error
	switch action {
	case ActionStart:
		err = c.api.ContainerStart(ctx, id, container.StartOptions{})
	case ActionStop:
		err = c.api.ContainerStop(ctx, id, container.StopOptions{Timeout: &stopTimeout})
	case ActionRestart:
		err = c.api.ContainerRestart(ctx, id, container.StopOptions{Timeout: &stopTimeout})
	case ActionKill:
		err = c.api.ContainerKill(ctx, id, "SIGKILL")
	case ActionPause:
		err = c.api.ContainerPause(ctx, id)
	case ActionUnpause:
		err = c.api.ContainerUnpause(ctx, id)
	case ActionRemove:
		// Volumes are deliberately preserved: removing a container should not
		// silently destroy a database.
		err = c.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	default:
		return fmt.Errorf("dockerx: unknown action %q", action)
	}

	if err != nil {
		return fmt.Errorf("dockerx: %s %s: %w", action, short(id), err)
	}
	return nil
}

// LogOptions controls a log request.
type LogOptions struct {
	// Tail is how many trailing lines to send before live output. Following
	// from the beginning of a long-running container would flood the browser.
	Tail   string
	Since  string
	Follow bool
}

// Logs streams container output into w.
//
// Containers without a TTY multiplex stdout and stderr into one stream with an
// 8-byte frame header per chunk. Writing that to a browser verbatim produces
// visible binary garbage every few lines, so it is demultiplexed here. This is
// the single most common thing to get wrong with the Engine API.
func (c *Client) Logs(ctx context.Context, id string, opts LogOptions, stdout, stderr io.Writer) error {
	details, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return fmt.Errorf("dockerx: inspect %s: %w", short(id), err)
	}

	tail := opts.Tail
	if tail == "" {
		tail = "200"
	}

	reader, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     opts.Follow,
		Tail:       tail,
		Since:      opts.Since,
	})
	if err != nil {
		return fmt.Errorf("dockerx: logs %s: %w", short(id), err)
	}
	defer func() { _ = reader.Close() }()

	// A TTY container writes a single already-merged stream with no framing.
	if details.Config != nil && details.Config.Tty {
		_, err = io.Copy(stdout, reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, reader)
	}

	// A cancelled context is the normal way a follower stops; it is not a
	// failure worth reporting.
	if err != nil && ctx.Err() == nil && !isClosedStream(err) {
		return fmt.Errorf("dockerx: stream logs %s: %w", short(id), err)
	}
	return nil
}

// Stats streams resource usage samples for one container.
func (c *Client) Stats(ctx context.Context, id string) (io.ReadCloser, error) {
	response, err := c.api.ContainerStats(ctx, id, true)
	if err != nil {
		return nil, fmt.Errorf("dockerx: stats %s: %w", short(id), err)
	}
	return response.Body, nil
}

// --- images, volumes, networks, disk usage ------------------------------

func (c *Client) ListImages(ctx context.Context) ([]image.Summary, error) {
	images, err := c.api.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("dockerx: list images: %w", err)
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Created > images[j].Created })
	return images, nil
}

func (c *Client) ListVolumes(ctx context.Context) ([]*volume.Volume, error) {
	response, err := c.api.VolumeList(ctx, volume.ListOptions{Filters: filters.NewArgs()})
	if err != nil {
		return nil, fmt.Errorf("dockerx: list volumes: %w", err)
	}
	return response.Volumes, nil
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	networks, err := c.api.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("dockerx: list networks: %w", err)
	}
	sort.Slice(networks, func(i, j int) bool { return networks[i].Name < networks[j].Name })
	return networks, nil
}

// Info returns daemon-level facts for the server overview.
func (c *Client) Info(ctx context.Context) (system Info, err error) {
	raw, err := c.api.Info(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("dockerx: info: %w", err)
	}
	return Info{
		ServerVersion:     raw.ServerVersion,
		OperatingSystem:   raw.OperatingSystem,
		Architecture:      raw.Architecture,
		CPUs:              raw.NCPU,
		MemoryBytes:       raw.MemTotal,
		Containers:        raw.Containers,
		ContainersRunning: raw.ContainersRunning,
		ContainersStopped: raw.ContainersStopped + raw.ContainersPaused,
		Images:            raw.Images,
	}, nil
}

// Info is the daemon summary shown on a server page.
type Info struct {
	ServerVersion     string `json:"server_version"`
	OperatingSystem   string `json:"operating_system"`
	Architecture      string `json:"architecture"`
	CPUs              int    `json:"cpus"`
	MemoryBytes       int64  `json:"memory_bytes"`
	Containers        int    `json:"containers"`
	ContainersRunning int    `json:"containers_running"`
	ContainersStopped int    `json:"containers_stopped"`
	Images            int    `json:"images"`
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// isClosedStream recognises the benign end of a followed stream.
func isClosedStream(err error) bool {
	text := err.Error()
	return strings.Contains(text, "use of closed") ||
		strings.Contains(text, "EOF") ||
		strings.Contains(text, "closed network connection")
}

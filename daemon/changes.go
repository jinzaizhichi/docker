package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/docker/docker/daemon/internal/metrics"
	"github.com/moby/go-archive"
)

// ContainerChanges returns a list of container fs changes
func (daemon *Daemon) ContainerChanges(ctx context.Context, name string) ([]archive.Change, error) {
	start := time.Now()

	container, err := daemon.GetContainer(name)
	if err != nil {
		return nil, err
	}

	if isWindows && container.IsRunning() {
		return nil, errors.New("Windows does not support diff of a running container")
	}

	c, err := daemon.imageService.Changes(ctx, container)
	if err != nil {
		return nil, err
	}
	metrics.ContainerActions.WithValues("changes").UpdateSince(start)
	return c, nil
}

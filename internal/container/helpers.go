package container

import (
	dockercontainer "github.com/docker/docker/api/types/container"
)

// containerConfig builds a Docker container.Config for validation/agent containers.
func containerConfig(image, user string, labels map[string]string) *dockercontainer.Config {
	return &dockercontainer.Config{
		Image:  image,
		User:   user,
		Labels: labels,
	}
}

// startOptions returns default Docker container start options.
func startOptions() dockercontainer.StartOptions {
	return dockercontainer.StartOptions{}
}

// removeOptions returns Docker container remove options.
func removeOptions(force bool) dockercontainer.RemoveOptions {
	return dockercontainer.RemoveOptions{Force: force}
}

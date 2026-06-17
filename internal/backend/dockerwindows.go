package backend

import "github.com/docker/docker/api/types/container"

// NewDockerWindows creates a backend bound to a Windows Docker daemon (a
// standalone dockerd.exe in Windows-container mode, typically on a custom named
// pipe such as npipe:////./pipe/docker_engine_windows). isolation is "process"
// (default) or "hyperv".
func NewDockerWindows(host, isolation string) (Backend, error) {
	iso := container.Isolation(isolation) // "", "process", "hyperv"
	return newDockerBackend("docker-windows", host, iso)
}

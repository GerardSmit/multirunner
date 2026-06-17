package backend

// NewDockerLinux creates a backend bound to a Linux Docker daemon (typically
// Docker Engine inside WSL2, reachable at tcp://127.0.0.1:2375). Linux
// containers use the daemon default isolation.
func NewDockerLinux(host string) (Backend, error) {
	return newDockerBackend("docker-linux", host, "")
}

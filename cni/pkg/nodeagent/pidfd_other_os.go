//go:build !(linux && (amd64 || arm64))

package nodeagent

const SYS_PIDFD_OPEN = 0

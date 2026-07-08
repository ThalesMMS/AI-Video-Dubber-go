package executil

import "os/exec"

// ConfigureProcess applies platform-specific subprocess isolation.
func ConfigureProcess(cmd *exec.Cmd) { configureProcess(cmd) }

// TerminateProcess stops a configured subprocess and its children when possible.
func TerminateProcess(cmd *exec.Cmd) { terminateProcess(cmd) }

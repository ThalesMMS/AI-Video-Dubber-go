// Package executil runs external tools with cancellation and line-oriented logs.
package executil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// LogFunc receives one complete output line.
type LogFunc func(string)

// Runner executes external commands.
type Runner struct {
	Log   LogFunc
	Tools map[string]string
	Env   []string
}

// Options configures one command invocation.
type Options struct {
	Dir       string
	Env       []string
	Stdin     io.Reader
	Quiet     bool
	ErrorTail int
}

// CommandError includes the executable, exit status, and recent output.
type CommandError struct {
	Name   string
	Args   []string
	Err    error
	Output string
}

func (e *CommandError) Error() string {
	message := fmt.Sprintf("command failed: %s", e.Name)
	var exitErr *exec.ExitError
	if errors.As(e.Err, &exitErr) {
		message += fmt.Sprintf(" (exit code %d)", exitErr.ExitCode())
	} else if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	if strings.TrimSpace(e.Output) != "" {
		message += "\n" + strings.TrimSpace(e.Output)
	}
	return message
}

func (e *CommandError) Unwrap() error { return e.Err }

// Run executes a command and streams stdout/stderr to the configured logger.
func (r Runner) Run(ctx context.Context, name string, args []string, options Options) error {
	if options.ErrorTail <= 0 {
		options.ErrorTail = 32 * 1024
	}
	commandName := r.commandName(name)
	cmd := exec.Command(commandName, args...)
	configureProcess(cmd)
	cmd.Dir = options.Dir
	if env := r.commandEnv(options.Env); len(env) > 0 {
		cmd.Env = env
	}
	cmd.Stdin = options.Stdin

	writer := newLineWriter(r.Log, options.Quiet, options.ErrorTail)
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		return &CommandError{Name: commandName, Args: append([]string(nil), args...), Err: err}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		terminateProcess(cmd)
		<-done
		err = ctx.Err()
	}
	writer.Flush()
	if err != nil {
		return &CommandError{
			Name:   commandName,
			Args:   append([]string(nil), args...),
			Err:    err,
			Output: writer.Tail(),
		}
	}
	return nil
}

// Output executes a command and captures combined output without logging it.
func (r Runner) Output(ctx context.Context, name string, args []string, options Options) (string, error) {
	var buffer bytes.Buffer
	options.Quiet = true
	commandName := r.commandName(name)
	cmd := exec.Command(commandName, args...)
	configureProcess(cmd)
	cmd.Dir = options.Dir
	if env := r.commandEnv(options.Env); len(env) > 0 {
		cmd.Env = env
	}
	cmd.Stdin = options.Stdin
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer

	if err := cmd.Start(); err != nil {
		return "", &CommandError{Name: commandName, Args: append([]string(nil), args...), Err: err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		terminateProcess(cmd)
		<-done
		err = ctx.Err()
	}
	if err != nil {
		return buffer.String(), &CommandError{Name: commandName, Args: append([]string(nil), args...), Err: err, Output: RedactSecrets(buffer.String())}
	}
	return buffer.String(), nil
}

func (r Runner) commandName(name string) string {
	if r.Tools != nil {
		if resolved := strings.TrimSpace(r.Tools[name]); resolved != "" {
			return resolved
		}
	}
	return name
}

func (r Runner) commandEnv(optionEnv []string) []string {
	if len(r.Env) == 0 && len(optionEnv) == 0 {
		return nil
	}
	env := os.Environ()
	env = append(env, r.Env...)
	env = append(env, optionEnv...)
	return env
}

// Require verifies that an executable is available in PATH.
func Require(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required executable %q was not found in PATH", name)
	}
	return nil
}

type lineWriter struct {
	mu       sync.Mutex
	pending  []byte
	tail     []byte
	tailSize int
	log      LogFunc
	quiet    bool
}

func newLineWriter(log LogFunc, quiet bool, tailSize int) *lineWriter {
	return &lineWriter{log: log, quiet: quiet, tailSize: tailSize}
}

func (w *lineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.appendTail(data)
	w.pending = append(w.pending, data...)
	for {
		index, width := lineBreak(w.pending)
		if index < 0 {
			break
		}
		line := RedactSecrets(strings.TrimRight(string(w.pending[:index]), "\r"))
		w.pending = w.pending[index+width:]
		if !w.quiet && w.log != nil && strings.TrimSpace(line) != "" {
			w.log(line)
		}
	}
	return len(data), nil
}

func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 && !w.quiet && w.log != nil {
		line := RedactSecrets(strings.TrimRight(string(w.pending), "\r\n"))
		if strings.TrimSpace(line) != "" {
			w.log(line)
		}
	}
	w.pending = nil
}

func lineBreak(data []byte) (int, int) {
	for index, value := range data {
		switch value {
		case '\n':
			return index, 1
		case '\r':
			if index+1 < len(data) && data[index+1] == '\n' {
				return index, 2
			}
			return index, 1
		}
	}
	return -1, 0
}

func (w *lineWriter) Tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return RedactSecrets(string(append([]byte(nil), w.tail...)))
}

func (w *lineWriter) appendTail(data []byte) {
	if w.tailSize <= 0 {
		return
	}
	w.tail = append(w.tail, data...)
	if len(w.tail) > w.tailSize {
		w.tail = append([]byte(nil), w.tail[len(w.tail)-w.tailSize:]...)
	}
}

package executor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

type DockerOptions struct {
	WorkspaceRoot string
	AllowedImages []string
	Artifacts     *artifact.Store
}

type dockerProcess struct {
	mu          sync.Mutex
	job         Job
	result      Result
	cancel      context.CancelFunc
	done        chan struct{}
	logs        []LogEntry
	subscribers map[chan LogEntry]bool
}

type DockerExecutor struct {
	root      string
	images    []string
	artifacts *artifact.Store
	mu        sync.Mutex
	jobs      map[string]*dockerProcess
}

func NewDocker(options DockerOptions) (*DockerExecutor, error) {
	if options.WorkspaceRoot == "" || len(options.AllowedImages) == 0 {
		return nil, errors.New("workspace root and an image allowlist are required")
	}
	root, err := filepath.Abs(options.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &DockerExecutor{root: root, images: append([]string(nil), options.AllowedImages...), artifacts: options.Artifacts, jobs: map[string]*dockerProcess{}}, nil
}

func (d *DockerExecutor) Submit(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validJobID(job.ID) || len(job.Command) == 0 || !slices.Contains(d.images, job.Image) {
		return errors.New("invalid job ID, command, or image")
	}
	if job.Timeout <= 0 {
		job.Timeout = 5 * time.Minute
	}
	if job.MemoryMB <= 0 {
		job.MemoryMB = 512
	}
	if job.CPUs <= 0 {
		job.CPUs = 1
	}
	if job.PIDs <= 0 {
		job.PIDs = 128
	}
	if job.Network != "" && job.Network != "none" {
		return errors.New("Docker executor currently requires network=none")
	}
	workspace, err := d.secureWorkspace(job.Workspace)
	if err != nil {
		return err
	}
	job.Workspace = workspace
	d.mu.Lock()
	if _, exists := d.jobs[job.ID]; exists {
		d.mu.Unlock()
		return errors.New("job ID already exists")
	}
	process := &dockerProcess{job: job, result: Result{ID: job.ID, State: StatePending}, done: make(chan struct{}), subscribers: map[chan LogEntry]bool{}}
	d.jobs[job.ID] = process
	d.mu.Unlock()
	jobContext, cancel := context.WithTimeout(context.Background(), job.Timeout)
	process.cancel = cancel
	containerName := containerName(job.ID)
	arguments := []string{"run", "--rm", "--name", containerName, "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--user=65532:65532", "--pids-limit", strconv.Itoa(job.PIDs), "--memory", fmt.Sprintf("%dm", job.MemoryMB), "--cpus", fmt.Sprintf("%.3f", job.CPUs), "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m", "--mount", "type=bind,src=" + workspace + ",dst=/work", "--workdir", "/work", "--env", "PYTHONDONTWRITEBYTECODE=1", "--env", "PYTHONUNBUFFERED=1"}
	keys := make([]string, 0, len(job.Env))
	for key := range job.Env {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			cancel()
			return errors.New("invalid environment variable name")
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		arguments = append(arguments, "--env", key)
	}
	arguments = append(arguments, job.Image, "sh", "-c", "umask 000 && exec \"$@\"", "autoresearch")
	arguments = append(arguments, job.Command...)
	command := exec.CommandContext(jobContext, "docker", arguments...)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONUNBUFFERED=1")
	for _, key := range keys {
		command.Env = append(command.Env, key+"="+job.Env[key])
	}
	command.Cancel = func() error {
		killContext, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer killCancel()
		_ = exec.CommandContext(killContext, "docker", "kill", containerName).Run()
		return command.Process.Kill()
	}
	command.WaitDelay = 5 * time.Second
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	process.result.State = StateRunning
	process.result.StartedAt = time.Now().UTC()
	if err := command.Start(); err != nil {
		cancel()
		process.result.State = StateFailed
		process.result.Error = err.Error()
		process.result.EndedAt = time.Now().UTC()
		close(process.done)
		return err
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go d.readLogs(process, stdout, "stdout", &readers)
	go d.readLogs(process, stderr, "stderr", &readers)
	go func() {
		err := command.Wait()
		readers.Wait()
		process.mu.Lock()
		process.result.EndedAt = time.Now().UTC()
		process.result.ExitCode = command.ProcessState.ExitCode()
		switch {
		case errors.Is(jobContext.Err(), context.DeadlineExceeded):
			process.result.State = StateFailed
			process.result.Error = context.DeadlineExceeded.Error()
		case errors.Is(jobContext.Err(), context.Canceled):
			process.result.State = StateCancelled
			process.result.Error = context.Canceled.Error()
		case err != nil:
			process.result.State = StateFailed
			process.result.Error = err.Error()
		default:
			process.result.State = StateCompleted
		}
		for subscriber := range process.subscribers {
			close(subscriber)
			delete(process.subscribers, subscriber)
		}
		close(process.done)
		process.mu.Unlock()
		cancel()
	}()
	return nil
}

func (d *DockerExecutor) Status(_ context.Context, id string) (Result, error) {
	process, err := d.process(id)
	if err != nil {
		return Result{}, err
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result, nil
}

func (d *DockerExecutor) Cancel(_ context.Context, id string) error {
	process, err := d.process(id)
	if err != nil {
		return err
	}
	process.mu.Lock()
	cancel := process.cancel
	process.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (d *DockerExecutor) Wait(ctx context.Context, id string) (Result, error) {
	process, err := d.process(id)
	if err != nil {
		return Result{}, err
	}
	select {
	case <-process.done:
		process.mu.Lock()
		defer process.mu.Unlock()
		return process.result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (d *DockerExecutor) Logs(ctx context.Context, id string) (<-chan LogEntry, error) {
	process, err := d.process(id)
	if err != nil {
		return nil, err
	}
	process.mu.Lock()
	channel := make(chan LogEntry, 1024)
	for _, entry := range process.logs {
		channel <- entry
	}
	select {
	case <-process.done:
		close(channel)
	default:
		process.subscribers[channel] = true
	}
	process.mu.Unlock()
	go func() {
		<-ctx.Done()
		process.mu.Lock()
		if process.subscribers[channel] {
			delete(process.subscribers, channel)
			close(channel)
		}
		process.mu.Unlock()
	}()
	return channel, nil
}

func (d *DockerExecutor) Collect(ctx context.Context, id string) ([]artifact.Record, error) {
	if d.artifacts == nil {
		return nil, errors.New("artifact storage is not configured")
	}
	process, err := d.process(id)
	if err != nil {
		return nil, err
	}
	select {
	case <-process.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	output := filepath.Join(process.job.Workspace, "output")
	if _, err := os.Stat(output); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	collection := artifactCollection{store: d.artifacts, runID: process.job.RunID}
	err = filepath.WalkDir(output, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("artifact symlinks are not allowed")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(process.job.Workspace, path)
		if err != nil {
			return err
		}
		return collection.put(ctx, relative, info.Size(), func() ([]byte, error) {
			return os.ReadFile(path)
		})
	})
	return collection.records, err
}

func (d *DockerExecutor) process(id string) (*dockerProcess, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	process := d.jobs[id]
	if process == nil {
		return nil, ErrJobNotFound
	}
	return process, nil
}

func (d *DockerExecutor) readLogs(process *dockerProcess, reader io.Reader, stream string, wait *sync.WaitGroup) {
	defer wait.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		process.mu.Lock()
		entry := LogEntry{Sequence: uint64(len(process.logs) + 1), Stream: stream, Text: scanner.Text() + "\n", Timestamp: time.Now().UTC()}
		process.logs = append(process.logs, entry)
		for subscriber := range process.subscribers {
			select {
			case subscriber <- entry:
			case <-process.done:
			}
		}
		process.mu.Unlock()
	}
}

func (d *DockerExecutor) secureWorkspace(workspace string) (string, error) {
	if workspace == "" {
		return "", errors.New("workspace is required")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(d.root, absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", errors.New("workspace is outside the executor root")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	relative, err = filepath.Rel(d.root, resolved)
	if err != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", errors.New("workspace symlink escapes the executor root")
	}
	if err := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("workspace symlinks are not allowed")
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o755)
		}
		return os.Chmod(path, 0o644)
	}); err != nil {
		return "", err
	}
	if err := os.Chmod(resolved, 0o777); err != nil {
		return "", err
	}
	return resolved, nil
}

func containerName(id string) string {
	digest := sha256.Sum256([]byte(id + strconv.FormatInt(time.Now().UnixNano(), 10)))
	return "autoresearch-" + hex.EncodeToString(digest[:12])
}

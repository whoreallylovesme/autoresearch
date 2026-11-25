package executor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

type SSHOptions struct {
	Binary              string
	WorkspaceRoot       string
	RemoteRoot          string
	Destination         string
	AllowedDestinations []string
	IdentityFile        string
	KnownHostsFile      string
	Artifacts           *artifact.Store
}

type sshProcess struct {
	mu        sync.Mutex
	job       Job
	remoteDir string
	pid       int
	result    Result
}

type SSHExecutor struct {
	options SSHOptions
	mu      sync.Mutex
	jobs    map[string]*sshProcess
}

func NewSSH(options SSHOptions) (*SSHExecutor, error) {
	if options.Binary == "" {
		options.Binary = "ssh"
	}
	if options.WorkspaceRoot == "" || !path.IsAbs(options.RemoteRoot) || options.IdentityFile == "" || options.KnownHostsFile == "" || options.Artifacts == nil {
		return nil, errors.New("SSH workspace, absolute remote root, identity, known hosts, and artifacts are required")
	}
	if !slices.Contains(options.AllowedDestinations, options.Destination) {
		return nil, errors.New("SSH destination is not allowlisted")
	}
	for _, file := range []string{options.IdentityFile, options.KnownHostsFile} {
		if _, err := os.Stat(file); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(options.WorkspaceRoot, 0o700); err != nil {
		return nil, err
	}
	return &SSHExecutor{options: options, jobs: map[string]*sshProcess{}}, nil
}

func (s *SSHExecutor) Submit(ctx context.Context, job Job) error {
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,96}$`).MatchString(job.ID) || len(job.Command) == 0 {
		return errors.New("valid job ID and command are required")
	}
	workspace, err := s.localWorkspace(job.Workspace)
	if err != nil {
		return err
	}
	if job.Timeout <= 0 {
		job.Timeout = 5 * time.Minute
	}
	s.mu.Lock()
	if _, exists := s.jobs[job.ID]; exists {
		s.mu.Unlock()
		return errors.New("job ID already exists")
	}
	remoteDir := path.Join(s.options.RemoteRoot, job.ID)
	process := &sshProcess{job: job, remoteDir: remoteDir, result: Result{ID: job.ID, State: StatePending}}
	s.jobs[job.ID] = process
	s.mu.Unlock()
	archive, err := archiveWorkspace(workspace)
	if err != nil {
		return err
	}
	if _, err := s.runSSH(ctx, "mkdir -p "+shellQuote(remoteDir)+" && tar -xzf - -C "+shellQuote(remoteDir), bytes.NewReader(archive)); err != nil {
		return err
	}
	command := remoteCommand(job.Command, remoteDir)
	script := command + "; code=$?; printf '%s' \"$code\" > .exit_code"
	launch := "cd " + shellQuote(remoteDir) + "; nohup sh -c " + shellQuote(script) + " > .autoresearch.log 2>&1 < /dev/null & echo $!"
	output, err := s.runSSH(ctx, launch, nil)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || pid <= 0 {
		return errors.New("SSH executor did not return a valid remote PID")
	}
	s.mu.Lock()
	process.pid = pid
	process.mutate(func(result *Result) {
		result.State = StateRunning
		result.StartedAt = time.Now().UTC()
	})
	s.mu.Unlock()
	return nil
}

func (s *SSHExecutor) Status(ctx context.Context, id string) (Result, error) {
	process, err := s.process(id)
	if err != nil {
		return Result{}, err
	}
	if process.running() {
		_ = s.refresh(ctx, process)
	}
	return process.resultSnapshot(), nil
}

func (s *SSHExecutor) Wait(ctx context.Context, id string) (Result, error) {
	process, err := s.process(id)
	if err != nil {
		return Result{}, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !process.running() {
			return process.resultSnapshot(), nil
		}
		if err := s.refresh(ctx, process); err != nil {
			return Result{}, err
		}
		if !process.running() {
			return process.resultSnapshot(), nil
		}
		result := process.resultSnapshot()
		if time.Now().After(result.StartedAt.Add(process.job.Timeout)) {
			_ = s.Cancel(ctx, id)
			process.mutate(func(result *Result) {
				result.State = StateFailed
				result.Error = context.DeadlineExceeded.Error()
			})
			return process.resultSnapshot(), nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *SSHExecutor) Cancel(ctx context.Context, id string) error {
	process, err := s.process(id)
	if err != nil {
		return err
	}
	if !process.running() {
		return nil
	}
	_, killErr := s.runSSH(ctx, "kill "+strconv.Itoa(process.pid), nil)
	process.mutate(func(result *Result) {
		result.State = StateCancelled
		result.Error = "remote cancellation requested"
		result.EndedAt = time.Now().UTC()
	})
	return killErr
}

func (s *SSHExecutor) Logs(ctx context.Context, id string) (<-chan LogEntry, error) {
	process, err := s.process(id)
	if err != nil {
		return nil, err
	}
	channel := make(chan LogEntry, 64)
	go func() {
		defer close(channel)
		sent := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			content, _ := s.runSSH(ctx, "cat "+shellQuote(path.Join(process.remoteDir, ".autoresearch.log")), nil)
			lines := strings.SplitAfter(string(content), "\n")
			for sent < len(lines) && lines[sent] != "" {
				sent++
				entry := LogEntry{Sequence: uint64(sent), Stream: "combined", Text: lines[sent-1], Timestamp: time.Now().UTC()}
				select {
				case channel <- entry:
				case <-ctx.Done():
					return
				}
			}
			if !process.running() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return channel, nil
}

func (s *SSHExecutor) Collect(ctx context.Context, id string) ([]artifact.Record, error) {
	process, err := s.process(id)
	if err != nil {
		return nil, err
	}
	if process.running() {
		if _, err := s.Wait(ctx, id); err != nil {
			return nil, err
		}
	}
	content, err := s.runSSH(ctx, "cd "+shellQuote(process.remoteDir)+" && tar -czf - output", nil)
	if err != nil {
		return nil, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	archive := tar.NewReader(reader)
	var records []artifact.Record
	var total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		clean := path.Clean(header.Name)
		if path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
			return nil, errors.New("remote artifact path traversal")
		}
		if header.FileInfo().IsDir() {
			continue
		}
		if !header.FileInfo().Mode().IsRegular() || header.Size > 16<<20 || total+header.Size > 64<<20 || len(records) >= 20 {
			return nil, errors.New("remote artifact type, count, or size is invalid")
		}
		data, err := io.ReadAll(io.LimitReader(archive, header.Size+1))
		if err != nil {
			return nil, err
		}
		record, err := s.options.Artifacts.Put(ctx, process.job.RunID, clean, "application/octet-stream", data)
		if err != nil {
			return nil, err
		}
		total += header.Size
		records = append(records, record)
	}
	return records, nil
}

func (s *SSHExecutor) refresh(ctx context.Context, process *sshProcess) error {
	output, err := s.runSSH(ctx, "cat "+shellQuote(path.Join(process.remoteDir, ".exit_code")), nil)
	if err != nil {
		return nil
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return errors.New("invalid remote exit code")
	}
	process.mutate(func(result *Result) {
		result.ExitCode = code
		result.EndedAt = time.Now().UTC()
		if code == 0 {
			result.State = StateCompleted
		} else {
			result.State = StateFailed
			result.Error = fmt.Sprintf("remote command exited with code %d", code)
		}
	})
	return nil
}

func (p *sshProcess) resultSnapshot() Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}

func (p *sshProcess) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result.State == StateRunning
}

func (p *sshProcess) mutate(update func(*Result)) {
	p.mu.Lock()
	update(&p.result)
	p.mu.Unlock()
}

func (s *SSHExecutor) process(id string) (*sshProcess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	process := s.jobs[id]
	if process == nil {
		return nil, ErrJobNotFound
	}
	return process, nil
}

func (s *SSHExecutor) runSSH(ctx context.Context, remoteCommand string, input io.Reader) ([]byte, error) {
	arguments := []string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + s.options.KnownHostsFile, "-i", s.options.IdentityFile, s.options.Destination, remoteCommand}
	command := exec.CommandContext(ctx, s.options.Binary, arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin"}
	command.Stdin = input
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("SSH remote command failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func (s *SSHExecutor) localWorkspace(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(s.options.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("workspace is outside SSH root")
	}
	return absolute, nil
}

func archiveWorkspace(workspace string) ([]byte, error) {
	var output bytes.Buffer
	compressor := gzip.NewWriter(&output)
	archive := tar.NewWriter(compressor)
	var total int64
	err := filepath.WalkDir(workspace, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("workspace symlinks are not allowed")
		}
		relative, err := filepath.Rel(workspace, file)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return archive.WriteHeader(&tar.Header{Name: filepath.ToSlash(relative) + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 || total+info.Size() > 64<<20 {
			return errors.New("workspace file type or size is invalid")
		}
		if err := archive.WriteHeader(&tar.Header{Name: filepath.ToSlash(relative), Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		content, err := os.Open(file)
		if err != nil {
			return err
		}
		defer content.Close()
		if _, err := io.Copy(archive, content); err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressor.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func remoteCommand(command []string, remoteDir string) string {
	quoted := make([]string, 0, len(command))
	for _, argument := range command {
		if argument == "/work" || strings.HasPrefix(argument, "/work/") {
			argument = strings.Replace(argument, "/work", remoteDir, 1)
		}
		quoted = append(quoted, shellQuote(argument))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

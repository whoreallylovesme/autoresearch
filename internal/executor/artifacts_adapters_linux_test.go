//go:build linux

package executor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDockerCollectRejectsLimitsBeforeReading(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string) string
		want  string
	}{
		{
			name: "oversized file",
			setup: func(t *testing.T, output string) string {
				path := filepath.Join(output, "00")
				writeSizedArtifact(t, path, 16<<20+1)
				return path
			},
			want: "artifact size limit exceeded",
		},
		{
			name: "aggregate overflow",
			setup: func(t *testing.T, output string) string {
				for index := 0; index < 4; index++ {
					writeSizedArtifact(t, filepath.Join(output, fmt.Sprintf("%02d", index)), 16<<20)
				}
				path := filepath.Join(output, "04")
				if err := os.WriteFile(path, []byte{'x'}, 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			want: "artifact size limit exceeded",
		},
		{
			name: "21st entry",
			setup: func(t *testing.T, output string) string {
				for index := 0; index < 20; index++ {
					if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("%02d", index)), []byte("artifact"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(output, "20")
				if err := os.WriteFile(path, []byte("must not be read"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			want: "too many artifacts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			output := filepath.Join(workspace, "output")
			if err := os.Mkdir(output, 0o700); err != nil {
				t.Fatal(err)
			}
			offending := test.setup(t, output)
			readEvents := watchArtifactReads(t, offending)
			runner := newDockerArtifactTestExecutor(t, workspace)
			if _, err := runner.Collect(context.Background(), "job"); err == nil || err.Error() != test.want {
				t.Fatalf("Collect() error = %v, want %q", err, test.want)
			}
			requireNoArtifactRead(t, readEvents)
		})
	}
}

func TestSSHCollectRejectsLimitsBeforeReading(t *testing.T) {
	tests := []struct {
		name     string
		complete []remoteArtifactTestEntry
		pending  *tar.Header
		want     string
	}{
		{
			name:    "oversized file",
			pending: remoteArtifactTestHeader("output/00", 16<<20+1),
			want:    "artifact size limit exceeded",
		},
		{
			name: "aggregate overflow",
			complete: []remoteArtifactTestEntry{
				{name: "output/00", size: 16 << 20},
				{name: "output/01", size: 16 << 20},
				{name: "output/02", size: 16 << 20},
				{name: "output/03", size: 16 << 20},
			},
			pending: remoteArtifactTestHeader("output/04", 1),
			want:    "artifact size limit exceeded",
		},
		{
			name:     "21st entry",
			complete: completeRemoteArtifactTestEntries(20),
			pending:  remoteArtifactTestHeader("output/20", 1),
			want:     "too many artifacts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := remoteArtifactTestArchive(t, test.complete, test.pending)
			runner := newSSHArtifactTestExecutor(t, archive)
			if _, err := runner.Collect(context.Background(), "job"); err == nil || err.Error() != test.want {
				t.Fatalf("Collect() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestArtifactCollectRetainsAdapterProtectionsAndReadErrors(t *testing.T) {
	t.Run("Docker symlink", func(t *testing.T) {
		workspace := t.TempDir()
		output := filepath.Join(workspace, "output")
		if err := os.Mkdir(output, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "target"), []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target", filepath.Join(output, "00-link")); err != nil {
			t.Fatal(err)
		}
		runner := newDockerArtifactTestExecutor(t, workspace)
		if _, err := runner.Collect(context.Background(), "job"); err == nil || err.Error() != "artifact symlinks are not allowed" {
			t.Fatalf("Collect() error = %v, want symlink rejection", err)
		}
	})

	t.Run("SSH traversal", func(t *testing.T) {
		archive := remoteArtifactTestArchive(t, []remoteArtifactTestEntry{{name: "../escape", size: 1, content: []byte{'x'}}}, nil)
		runner := newSSHArtifactTestExecutor(t, archive)
		if _, err := runner.Collect(context.Background(), "job"); err == nil || err.Error() != "remote artifact path traversal" {
			t.Fatalf("Collect() error = %v, want traversal rejection", err)
		}
	})

	t.Run("SSH truncated regular file", func(t *testing.T) {
		archive := remoteArtifactTestArchive(t, nil, remoteArtifactTestHeader("output/00", 1))
		runner := newSSHArtifactTestExecutor(t, archive)
		if _, err := runner.Collect(context.Background(), "job"); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("Collect() error = %v, want io.ErrUnexpectedEOF", err)
		}
	})
}

func newDockerArtifactTestExecutor(t *testing.T, workspace string) *DockerExecutor {
	t.Helper()
	process := &dockerProcess{job: Job{ID: "job", RunID: "run", Workspace: workspace}, done: make(chan struct{})}
	close(process.done)
	return &DockerExecutor{artifacts: newArtifactTestStore(t), jobs: map[string]*dockerProcess{"job": process}}
}

func writeSizedArtifact(t *testing.T, path string, size int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func watchArtifactReads(t *testing.T, path string) int {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if _, err := syscall.InotifyAddWatch(fd, path, syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	return fd
}

func requireNoArtifactRead(t *testing.T, fd int) {
	t.Helper()
	buffer := make([]byte, 4096)
	n, err := syscall.Read(fd, buffer)
	if err == syscall.EAGAIN {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatal("rejected artifact was read before accounting validation")
	}
}

type remoteArtifactTestEntry struct {
	name    string
	size    int64
	content []byte
}

func remoteArtifactTestHeader(name string, size int64) *tar.Header {
	return &tar.Header{Name: name, Mode: 0o600, Size: size, Typeflag: tar.TypeReg}
}

func completeRemoteArtifactTestEntries(count int) []remoteArtifactTestEntry {
	entries := make([]remoteArtifactTestEntry, 0, count)
	for index := 0; index < count; index++ {
		entries = append(entries, remoteArtifactTestEntry{name: fmt.Sprintf("output/%02d", index), size: 1, content: []byte{'x'}})
	}
	return entries
}

func remoteArtifactTestArchive(t *testing.T, entries []remoteArtifactTestEntry, pending *tar.Header) []byte {
	t.Helper()
	var output bytes.Buffer
	compressor := gzip.NewWriter(&output)
	archive := tar.NewWriter(compressor)
	zeroes := bytes.Repeat([]byte{0}, 16<<20)
	for _, entry := range entries {
		if err := archive.WriteHeader(remoteArtifactTestHeader(entry.name, entry.size)); err != nil {
			t.Fatal(err)
		}
		content := entry.content
		if content == nil {
			content = zeroes[:entry.size]
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if pending != nil {
		if err := archive.WriteHeader(pending); err != nil {
			t.Fatal(err)
		}
	} else if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func newSSHArtifactTestExecutor(t *testing.T, archive []byte) *SSHExecutor {
	t.Helper()
	root := t.TempDir()
	archivePath := filepath.Join(root, "artifacts.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "ssh")
	content := "#!/bin/sh\n/bin/cat " + shellQuote(archivePath) + "\n"
	if err := os.WriteFile(binary, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	process := &sshProcess{job: Job{ID: "job", RunID: "run"}, remoteDir: "/unused", result: Result{State: StateCompleted}}
	return &SSHExecutor{options: SSHOptions{Binary: binary, Artifacts: newArtifactTestStore(t)}, jobs: map[string]*sshProcess{"job": process}}
}

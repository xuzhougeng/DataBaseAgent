package workbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type Observation struct {
	Status   string
	Log      string
	ExitCode int
}
type Executor interface {
	Probe(context.Context, Server) (string, error)
	Start(context.Context, Job) error
	Poll(context.Context, Job) (Observation, error)
	Sync(context.Context, Job, string) ([]Artifact, error)
}
type SSHExecutor struct{}

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }

func sshCommand(ctx context.Context, s Server, script string) *exec.Cmd {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=12", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2", "-p", strconv.Itoa(s.Port)}
	if s.KeyPath != "" {
		args = append(args, "-i", s.KeyPath)
	}
	args = append(args, s.User+"@"+s.Host, "sh", "-c", quote(script))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	hideCommand(cmd)
	return cmd
}
func remote(ctx context.Context, s Server, script string) (string, error) {
	cmd := sshCommand(ctx, s, script)
	var out, stderr limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("SSH: %w · %s", err, stderr.String()+out.String())
	}
	return out.String(), nil
}

type limitedBuffer struct{ buf bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= 128*1024 {
		b.buf.Reset()
		b.buf.Write(p[n-128*1024:])
		return n, nil
	}
	if b.buf.Len()+n > 128*1024 {
		old := append([]byte(nil), b.buf.Bytes()...)
		b.buf.Reset()
		b.buf.Write(old[len(old)-(128*1024-n):])
	}
	b.buf.Write(p)
	return n, nil
}
func (b *limitedBuffer) String() string { return b.buf.String() }

func (*SSHExecutor) Probe(ctx context.Context, s Server) (string, error) {
	return remote(ctx, s, "set -eu\nmkdir -p "+quote(s.WorkDir)+"\ntest -w "+quote(s.WorkDir)+"\nprintf 'SSH 与工作目录: OK\\n'\nuname -sr\nprintf 'CPU: '; getconf _NPROCESSORS_ONLN\ncommand -v nohup\ncommand -v sha256sum\ncommand -v realpath\ndf -h "+quote(s.WorkDir))
}

func runScript(j Job) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -u\n")
	vars := map[string]string{"DBA_ACCESSION": j.Accession, "DBA_SOURCE_URL": j.Input.DownloadURL, "DBA_INPUT": path.Join(path.Dir(j.RemoteDir), "input"), "DBA_OUTPUT": path.Join(j.RemoteDir, "output"), "DBA_SAMPLE_GROUPS": j.Input.Groups, "DBA_CPUS": strconv.Itoa(j.Pipeline.CPUs)}
	for _, name := range []string{"DBA_ACCESSION", "DBA_SOURCE_URL", "DBA_INPUT", "DBA_OUTPUT", "DBA_SAMPLE_GROUPS", "DBA_CPUS"} {
		b.WriteString("export " + name + "=" + quote(vars[name]) + "\n")
	}
	b.WriteString("mkdir -p \"$DBA_INPUT\" \"$DBA_OUTPUT\"\n(\nset -e\ncd \"$DBA_OUTPUT\"\n")
	for i, step := range j.Pipeline.Steps {
		b.WriteString("printf '%s\\n' " + quote(fmt.Sprintf("[DBA step %d] %s · %s", i+1, step.Name, step.Tool)) + "\n")
		b.WriteString(step.Command)
		b.WriteString("\n")
	}
	b.WriteString(")\nrc=$?\nprintf '%s\\n' \"$rc\" > " + quote(path.Join(j.RemoteDir, "exitcode.tmp")) + "\nmv " + quote(path.Join(j.RemoteDir, "exitcode.tmp")) + " " + quote(path.Join(j.RemoteDir, "exitcode")) + "\nexit \"$rc\"\n")
	return b.String()
}

func (*SSHExecutor) Start(ctx context.Context, j Job) error {
	// A durable submission marker prevents duplicate execution after a lost SSH acknowledgement.
	marker := "DBA_SCRIPT_" + strings.ReplaceAll(j.ID, "-", "") + "_" + strconv.Itoa(j.Attempt)
	script := "set -eu\nmkdir -p " + quote(j.RemoteDir) + "\ncd " + quote(j.RemoteDir) + "\nif [ -e submitted ]; then printf 'Already submitted\\n'; exit 0; fi\nmkdir .submit-lock || exit 75\ntrap 'rmdir .submit-lock' EXIT\nif [ -e submitted ]; then exit 0; fi\ncat > run.sh <<'" + marker + "'\n" + runScript(j) + marker + "\nprintf 'submitted\\n' > submitted\nnohup sh run.sh > run.log 2>&1 < /dev/null &\necho $! > pid\nprintf 'Submitted\\n'\n"
	_, e := remote(ctx, j.Server, script)
	return e
}

func (*SSHExecutor) Poll(ctx context.Context, j Job) (Observation, error) {
	script := "set -eu\ncd " + quote(j.RemoteDir) + "\nif [ -f exitcode ]; then printf 'EXIT:'; cat exitcode; elif [ -f pid ] && kill -0 \"$(cat pid)\" 2>/dev/null; then printf 'RUNNING\\n'; else printf 'UNKNOWN\\n'; fi\nprintf '\\n---DBA_LOG---\\n'\ntail -c 65536 run.log 2>/dev/null || true"
	out, e := remote(ctx, j.Server, script)
	if e != nil {
		return Observation{}, e
	}
	parts := strings.SplitN(out, "\n---DBA_LOG---\n", 2)
	status := strings.TrimSpace(parts[0])
	obs := Observation{Status: "running"}
	if len(parts) > 1 {
		obs.Log = parts[1]
	}
	if strings.HasPrefix(status, "EXIT:") {
		code, e := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(status, "EXIT:")))
		if e != nil {
			return obs, errors.New("远端退出码无效")
		}
		obs.ExitCode = code
		obs.Status = "failed"
		if code == 0 {
			obs.Status = "succeeded"
		}
	} else if status != "RUNNING" {
		obs.Status = "unknown"
	}
	return obs, nil
}

func remoteFileScript(j Job, name string) string {
	root := path.Join(j.RemoteDir, "output")
	file := path.Join(root, name)
	return "set -eu\nbase=$(realpath " + quote(root) + ")\nfile=$(realpath " + quote(file) + ")\ncase \"$file\" in \"$base\"/*) ;; *) exit 65 ;; esac\ntest -f \"$file\"\n"
}

func (*SSHExecutor) Sync(ctx context.Context, j Job, root string) ([]Artifact, error) {
	dir := filepath.Join(root, "projects", j.ProjectID, "results", j.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	artifacts := []Artifact{}
	for _, name := range j.Pipeline.Outputs {
		if !safeRelative(name) {
			return nil, errors.New("无效的产物路径")
		}
		check := remoteFileScript(j, name)
		out, e := remote(ctx, j.Server, check+"sha256sum \"$file\"\nwc -c < \"$file\"")
		if e != nil {
			return nil, e
		}
		fields := strings.Fields(out)
		if len(fields) < 3 || len(fields[0]) != 64 {
			return nil, errors.New("远端校验清单无效")
		}
		expected := strings.ToLower(fields[0])
		if _, e := hex.DecodeString(expected); e != nil {
			return nil, e
		}
		size, e := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if e != nil || size < 0 || size > 100<<30 {
			return nil, errors.New("产物大小无效或超出 100 GiB 单文件限制")
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return nil, e
		}
		if existing, e := hashFile(target); e == nil && existing == expected {
			info, _ := os.Stat(target)
			artifacts = append(artifacts, Artifact{ID: j.ID + ":" + name, JobID: j.ID, ProjectID: j.ProjectID, Accession: j.Accession, Name: name, Path: target, SHA256: expected, Size: info.Size(), At: now()})
			continue
		}
		part := target + ".partial"
		file, e := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		info, _ := file.Stat()
		offset := info.Size()
		if offset > size {
			if e = file.Truncate(0); e != nil {
				file.Close()
				return nil, e
			}
			offset = 0
		}
		if _, e = file.Seek(offset, io.SeekStart); e != nil {
			file.Close()
			return nil, e
		}
		cmd := sshCommand(ctx, j.Server, check+"tail -c +"+strconv.FormatInt(offset+1, 10)+" \"$file\"")
		stdout, e := cmd.StdoutPipe()
		if e != nil {
			file.Close()
			return nil, e
		}
		var stderr limitedBuffer
		cmd.Stderr = &stderr
		if e = cmd.Start(); e != nil {
			file.Close()
			return nil, e
		}
		n, copyErr := io.Copy(file, io.LimitReader(stdout, size-offset+1))
		waitErr := cmd.Wait()
		syncErr := file.Sync()
		file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if waitErr != nil {
			return nil, fmt.Errorf("产物传输失败: %w · %s", waitErr, stderr.String())
		}
		if syncErr != nil {
			return nil, syncErr
		}
		if offset+n != size {
			return nil, errors.New("产物大小校验失败，将重试传输")
		}
		actual, e := hashFile(part)
		if e != nil {
			return nil, e
		}
		if actual != expected {
			_ = os.Truncate(part, 0)
			return nil, errors.New("SHA-256 校验失败，已重置不完整传输，等待重试")
		}
		if e = os.Rename(part, target); e != nil {
			return nil, e
		}
		artifacts = append(artifacts, Artifact{ID: j.ID + ":" + name, JobID: j.ID, ProjectID: j.ProjectID, Accession: j.Accession, Name: name, Path: target, SHA256: actual, Size: size, At: now()})
	}
	return artifacts, nil
}

func hashFile(name string) (string, error) {
	f, e := os.Open(name)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

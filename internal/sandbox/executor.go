package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/acassiovilasboas/genoma/internal/core"
)

// Executor manages sandboxed script execution using Docker containers.
type Executor struct {
	dockerClient *client.Client
	sandboxImage string
	defaults     ResourceLimits
	security     *SecurityChecker
}

// NewExecutor creates a new sandbox executor.
func NewExecutor(dockerHost, sandboxImage string, defaults ResourceLimits) (*Executor, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if dockerHost != "" {
		opts = append(opts, client.WithHost(dockerHost))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &Executor{
		dockerClient: cli,
		sandboxImage: sandboxImage,
		defaults:     defaults,
		security:     NewSecurityChecker(0),
	}, nil
}

// Execute runs a script in an isolated Docker container and returns the result.
func (e *Executor) Execute(ctx context.Context, req core.ExecutionRequest) (*core.ExecutionResult, error) {
	startTime := time.Now()

	// 1. Pre-execution security check
	lang := string(req.Language)
	if err := e.security.Check(req.Script, lang); err != nil {
		return nil, &core.ErrSandboxSecurity{Reason: err.Error()}
	}

	// 2. Merge limits
	limits := e.defaults
	if req.Limits != nil {
		limits = limits.Merge(&ResourceLimits{
			CPUQuota:        req.Limits.CPUQuota,
			MemoryBytes:     req.Limits.MemoryBytes,
			NetworkDisabled: req.Limits.NetworkDisabled,
			TimeoutSec:      req.Limits.TimeoutSec,
			MaxOutputBytes:  req.Limits.MaxOutputBytes,
			ReadOnlyRootfs:  req.Limits.ReadOnlyRootfs,
			PidsLimit:       req.Limits.PidsLimit,
		})
	}

	// 3. Determine command based on language
	var scriptFile, cmd string
	switch lang {
	case "python":
		scriptFile = "/workspace/script.py"
		cmd = fmt.Sprintf("python3 /workspace/sandbox_wrapper.py %s", scriptFile)
	case "nodejs":
		scriptFile = "/workspace/script.js"
		cmd = fmt.Sprintf("node /workspace/sandbox_wrapper.js %s", scriptFile)
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	// 4. Create container config
	containerConfig := &container.Config{
		Image:        e.sandboxImage,
		Cmd:          []string{"sh", "-c", cmd},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
		Tty:          false,
		User:         "65534:65534", // nobody
		WorkingDir:   "/workspace",
	}

	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			CPUQuota:  limits.CPUQuota,
			Memory:    limits.MemoryBytes,
			PidsLimit: &limits.PidsLimit,
		},
		ReadonlyRootfs: limits.ReadOnlyRootfs,
		SecurityOpt:    []string{"no-new-privileges"},
		CapDrop:        []string{"ALL"},
		// Tmpfs for writable directories in read-only rootfs
		Tmpfs: map[string]string{
			"/tmp":       "rw,noexec,nosuid,size=64m",
			"/workspace": "rw,noexec,nosuid,size=64m",
		},
	}

	if limits.NetworkDisabled {
		containerConfig.NetworkDisabled = true
	}

	// 5. Create container
	resp, err := e.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("create sandbox container: %w", err)
	}
	containerID := resp.ID

	// Ensure cleanup
	defer func() {
		removeCtx := context.Background()
		e.dockerClient.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true})
		slog.Debug("sandbox container removed", "container_id", containerID[:12])
	}()

	// 6. Copy script content to container
	scriptContent := req.Script
	if err := e.copyToContainer(ctx, containerID, scriptFile, scriptContent); err != nil {
		return nil, fmt.Errorf("copy script to sandbox: %w", err)
	}

	// 7. Attach to container stdin/stdout
	attachResp, err := e.dockerClient.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stdin:  true,
		Stdout: true,
		Stderr: true,
		Stream: true,
	})
	if err != nil {
		return nil, fmt.Errorf("attach to sandbox: %w", err)
	}
	defer attachResp.Close()

	// 8. Start container
	if err := e.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start sandbox container: %w", err)
	}

	slog.Debug("sandbox container started",
		"container_id", containerID[:12],
		"language", lang,
		"timeout", limits.TimeoutSec,
	)

	// 9. Send input via stdin
	inputData, err := FormatInput(req.Input, scriptFile)
	if err != nil {
		return nil, fmt.Errorf("format input: %w", err)
	}
	attachResp.Conn.Write(append(inputData, '\n'))
	attachResp.CloseWrite()

	// 10. Wait for completion with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutSec)*time.Second)
	defer cancel()

	statusCh, errCh := e.dockerClient.ContainerWait(timeoutCtx, containerID, container.WaitConditionNotRunning)

	var exitCode int
	select {
	case err := <-errCh:
		if err != nil {
			// Kill container on timeout
			e.dockerClient.ContainerKill(context.Background(), containerID, "SIGKILL")
			return nil, &core.ErrSandboxTimeout{TimeoutSec: limits.TimeoutSec}
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	case <-timeoutCtx.Done():
		e.dockerClient.ContainerKill(context.Background(), containerID, "SIGKILL")
		return nil, &core.ErrSandboxTimeout{TimeoutSec: limits.TimeoutSec}
	}

	// 11. Read stdout/stderr
	var stdout, stderr bytes.Buffer
	stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader)

	// Limit output size
	outStr := stdout.String()
	if int64(len(outStr)) > limits.MaxOutputBytes {
		outStr = outStr[:limits.MaxOutputBytes]
	}

	// 12. Parse output protocol
	output, logs, execError := ParseOutput(outStr)

	duration := time.Since(startTime)

	slog.Info("sandbox execution completed",
		"container_id", containerID[:12],
		"exit_code", exitCode,
		"duration", duration,
	)

	result := &core.ExecutionResult{
		Output:   output,
		Logs:     logs,
		ExitCode: exitCode,
		Duration: duration,
	}

	if execError != "" {
		result.Error = execError
	}
	if stderr.Len() > 0 {
		if result.Error != "" {
			result.Error += "\n"
		}
		result.Error += stderr.String()
	}

	if exitCode != 0 && result.Error == "" {
		result.Error = fmt.Sprintf("script exited with code %d", exitCode)
	}

	if exitCode != 0 {
		return result, &core.ErrSandboxExecution{
			Script:   req.Script[:min(100, len(req.Script))],
			ExitCode: exitCode,
			Stderr:   result.Error,
		}
	}

	return result, nil
}

// copyToContainer copies content into the container filesystem.
func (e *Executor) copyToContainer(ctx context.Context, containerID, path, content string) error {
	// Create a tar archive with the file
	var buf bytes.Buffer
	tw := newTarWriter(&buf)
	tw.writeFile(path, content)
	tw.close()

	return e.dockerClient.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}

// EnsureImage pulls the sandbox image if not present.
func (e *Executor) EnsureImage(ctx context.Context) error {
	_, _, err := e.dockerClient.ImageInspectWithRaw(ctx, e.sandboxImage)
	if err == nil {
		return nil // Image exists
	}

	slog.Info("pulling sandbox image", "image", e.sandboxImage)
	reader, err := e.dockerClient.ImagePull(ctx, e.sandboxImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull sandbox image: %w", err)
	}
	defer reader.Close()
	io.Copy(io.Discard, reader) // Wait for pull to complete
	return nil
}

// Ping verifies Docker daemon connectivity.
func (e *Executor) Ping(ctx context.Context) error {
	_, err := e.dockerClient.Ping(ctx)
	return err
}

// Close releases the Docker client resources.
func (e *Executor) Close() error {
	return e.dockerClient.Close()
}

// --- Tar helper ---

type tarWriter struct {
	buf *bytes.Buffer
}

func newTarWriter(buf *bytes.Buffer) *tarWriter {
	return &tarWriter{buf: buf}
}

func (tw *tarWriter) writeFile(name, content string) {
	data := []byte(content)

	// Minimal tar header (512 bytes)
	header := make([]byte, 512)
	copy(header[0:], name)                       // filename
	copy(header[100:], "0000644")                 // mode
	copy(header[108:], "0000000")                 // uid
	copy(header[116:], "0000000")                 // gid
	copy(header[124:], fmt.Sprintf("%011o", len(data))) // size
	copy(header[136:], fmt.Sprintf("%011o", time.Now().Unix())) // mtime
	header[156] = '0'                             // typeflag (regular file)

	// Calculate checksum
	copy(header[148:], "        ") // Initialize checksum field with spaces
	var sum int
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))

	tw.buf.Write(header)
	tw.buf.Write(data)

	// Pad to 512-byte boundary
	if remainder := len(data) % 512; remainder != 0 {
		tw.buf.Write(make([]byte, 512-remainder))
	}
}

func (tw *tarWriter) close() {
	// Two zero blocks to end the archive
	tw.buf.Write(make([]byte, 1024))
}

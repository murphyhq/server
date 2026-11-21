//go:build integration

package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var binaries string

func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "murphy-testenv-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	root, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "find module root:", err)
		return 1
	}
	build := exec.Command("go", "build", "-o", dir+string(filepath.Separator), "./cmd/...")
	build.Dir = strings.TrimSpace(string(root))
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build components:", err)
		return 1
	}

	binaries = dir
	return m.Run()
}

func Run(t *testing.T, component string, env map[string]string) {
	t.Helper()
	cmd := command(t, component, env, freeAddr(t))
	require.NoError(t, cmd.Run(), "%s failed", component)
}

func Start(t *testing.T, component string, env map[string]string) string {
	t.Helper()
	httpAddr, healthAddr := freeAddr(t), freeAddr(t)
	cmd := command(t, component, env, healthAddr)
	cmd.Env = append(cmd.Env, "MURPHY_HTTP_ADDR="+httpAddr)
	require.NoError(t, cmd.Start())

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	require.Eventually(t, func() bool {
		select {
		case <-exited:
			return false
		default:
		}
		return ready(t.Context(), healthAddr)
	}, 30*time.Second, 100*time.Millisecond, "%s did not become ready", component)
	return "http://" + httpAddr
}

func command(t *testing.T, component string, env map[string]string, healthAddr string) *exec.Cmd {
	cmd := exec.CommandContext(t.Context(), filepath.Join(binaries, component))
	cmd.Env = []string{"MURPHY_HEALTH_ADDR=" + healthAddr, "MURPHY_SHUTDOWN_TIMEOUT=5s"}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout, cmd.Stderr = t.Output(), t.Output()
	return cmd
}

func ready(ctx context.Context, addr string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return addr
}

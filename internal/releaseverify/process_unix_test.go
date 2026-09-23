//go:build !windows

package releaseverify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVerifyReapsTimedOutProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile, binary := filepath.Join(dir, "pid"), filepath.Join(dir, "server")
	script := fmt.Sprintf("#!/bin/sh\necho $$ > %s\nsleep 30\n", pidFile)
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Verify(ctx, binary, "1"); err == nil {
		t.Fatal("expected timeout")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process %d was not reaped: %v", pid, err)
	}
}

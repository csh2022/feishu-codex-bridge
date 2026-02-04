package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type SingleInstanceError struct {
	PID int
}

func (e *SingleInstanceError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("another instance is running (pid %d)", e.PID)
	}
	return "another instance is running"
}

func acquireSingleInstanceLock(configDir string) (*os.File, error) {
	lockPath := filepath.Join(configDir, "bridge.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		otherPID := readPIDFromFile(lockPath)
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &SingleInstanceError{PID: otherPID}
		}
		return nil, fmt.Errorf("lock file: %w", err)
	}

	// Record PID (best effort) so a later instance can show a useful hint.
	if err := f.Truncate(0); err == nil {
		if _, err := f.Seek(0, 0); err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			_ = f.Sync()
		}
	}

	return f, nil
}

func readPIDFromFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

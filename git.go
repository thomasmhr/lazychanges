package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// emptyTree is the hash of git's empty tree, the base of a repository without commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// git runs a read-only git command in dir. GIT_OPTIONAL_LOCKS=0 keeps it from
// refreshing the index, so it never competes for index.lock with the agents at work.
func git(dir string, args ...string) ([]byte, error) {
	return gitAllowing(dir, nil, args...)
}

// gitAllowing treats the listed exit codes as success (git diff --no-index exits 1 on a difference).
func gitAllowing(dir string, okCodes []int, args ...string) ([]byte, error) {
	full := append([]string{"-c", "core.quotepath=off", "--no-pager"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		for _, c := range okCodes {
			if exit.ExitCode() == c {
				return stdout.Bytes(), nil
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func splitZ(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

func headCommit(dir string) string {
	out, err := git(dir, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if err != nil {
		return emptyTree
	}
	return strings.TrimSpace(string(out))
}

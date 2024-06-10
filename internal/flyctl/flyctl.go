package flyctl

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

var flyctlPath string

func init() {
	path, err := exec.LookPath("flyctl")
	if err != nil {
		slog.Info("can't find flyctl in $PATH, assuming it's in $HOME/.fly/bin/flyctl")
		flyctlPath = os.ExpandEnv("${HOME}/.fly/bin/flyctl")
	}
	flyctlPath = path
}

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, flyctlPath, args...)
	cmd.Env = os.Environ()

	var stdin bytes.Buffer
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdin = &stdin
	cmd.Stdout = &stdout //io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = &stderr //io.MultiWriter(os.Stderr, &stderr)

	//fmt.Printf("running: %s %+v...\n", flyctlPath, args)
	if err := cmd.Run(); err != nil {
		return "", CommandError{
			Program: flyctlPath,
			Args:    args,
			Output:  stderr.String(),
		}
	}
	return stdout.String(), nil
}

func WireGuardCreate(ctx context.Context, org, region, name string) (string, error) {
	tmpFile, err := os.CreateTemp("", "flyctl-wireguard-create")
	if err != nil {
		return "", fmt.Errorf("can't make temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	fname := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("can't close temp file: %w", err)
	}
	os.Remove(fname)

	if _, err := run(ctx, "wireguard", "create", org, region, name, fname); err != nil {
		return "", fmt.Errorf("can't make wireguard config: %w", err)
	}

	conf, err := os.ReadFile(fname)
	if err != nil {
		return "", fmt.Errorf("can't read temp file: %w", err)
	}

	return strings.TrimSpace(string(conf)), nil
}

func WireGuardRemove(ctx context.Context, org, name string) error {
	_, err := run(ctx, "wireguard", "remove", org, name)
	return err
}

type CommandError struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Output  string   `json:"output"`
}

func (ce CommandError) Error() string {
	return fmt.Sprintf("flyctl: error running command: %s %+v:\n\n%s", ce.Program, ce.Args, ce.Output)
}

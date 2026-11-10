//go:build !linux

package main

import (
	"errors"
	"os"
	"os/exec"
)

var errPTYUnsupported = errors.New("当前平台不支持伪终端（仅支持 Linux / Android）")

func setPTYSize(master *os.File, cols, rows int) error { return errPTYUnsupported }

func startInPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) { return nil, errPTYUnsupported }

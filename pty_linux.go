package main

// 最小化的 Linux/Android 伪终端实现（仅依赖标准库 syscall）。
// GOOS=android 同样会编译 *_linux.go 文件，因此 Termux 下可直接使用。

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func ioctlFile(f *os.File, req uintptr, arg uintptr) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	// 通过 RawConn.Control 执行 ioctl，避免 f.Fd() 把描述符切换为阻塞模式，
	// 从而保证 Close() 能唤醒阻塞中的 Read。
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// openPTY 打开一对主/从伪终端设备。
func openPTY() (master *os.File, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			master.Close()
		}
	}()

	var unlock int32
	if err = ioctlFile(master, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		return nil, nil, fmt.Errorf("unlockpt: %w", err)
	}
	var n uint32
	if err = ioctlFile(master, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		return nil, nil, fmt.Errorf("ptsname: %w", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return master, slave, nil
}

// setPTYSize 设置终端窗口大小（会向前台进程组发送 SIGWINCH）。
func setPTYSize(master *os.File, cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}
	ws := struct{ Row, Col, X, Y uint16 }{uint16(rows), uint16(cols), 0, 0}
	return ioctlFile(master, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

// startInPTY 在新的伪终端中启动 cmd：新建会话并把从设备设为控制终端。
func startInPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	defer slave.Close()

	if cols > 0 && rows > 0 {
		setPTYSize(master, cols, rows)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, err
	}
	return master, nil
}

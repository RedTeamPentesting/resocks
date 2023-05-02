//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func prepareTerminal() (reset func(), err error) {
	var mode uint32

	stdoutHandle := windows.Handle(os.Stdout.Fd())

	err = windows.GetConsoleMode(stdoutHandle, &mode)
	if err != nil {
		return nil, fmt.Errorf("stdout: get console mode: %w", err)
	}

	err = windows.SetConsoleMode(stdoutHandle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	if err != nil {
		return nil, fmt.Errorf("stdout: set console mode: %w", err)
	}

	stderrHandle := windows.Handle(os.Stderr.Fd())

	err = windows.GetConsoleMode(stderrHandle, &mode)
	if err != nil {
		return func() {
			_ = windows.SetConsoleMode(stdoutHandle, mode)
		}, fmt.Errorf("stderr: get console mode: %w", err)
	}

	err = windows.SetConsoleMode(stderrHandle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	if err != nil {
		return func() {
			_ = windows.SetConsoleMode(stdoutHandle, mode)
		}, fmt.Errorf("stderr: set console mode: %w", err)
	}

	return func() {
		_ = windows.SetConsoleMode(stdoutHandle, mode)
		_ = windows.SetConsoleMode(stderrHandle, mode)
	}, nil
}

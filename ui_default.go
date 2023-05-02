//go:build !windows

package main

func prepareTerminal() (reset func(), err error) {
	return func() {}, nil // we only have to do this on Windows
}

//go:build !nogui && (windows || darwin)

package gui

func goosIsUnixDisplayRequired() bool { return false }

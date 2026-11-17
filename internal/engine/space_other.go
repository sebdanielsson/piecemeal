//go:build !linux && !darwin

package engine

func freeSpace(string) int64 { return -1 }

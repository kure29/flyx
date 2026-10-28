//go:build !unix

package main

func disableCoreDumps() error { return nil }

//go:build !darwin || !e2e

package platform

func HandleNativeCommand() bool { return false }

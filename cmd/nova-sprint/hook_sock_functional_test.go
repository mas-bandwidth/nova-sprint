//go:build functional

package main

// In the functional tier a test app's hook process listens on a real unix socket of
// the test's own (shortSock, hook_test.go); the unit tier's testHookSock refuses
// (main_test.go).
func init() { testHookSock = shortSock }

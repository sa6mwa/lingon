// Package testpty provides test-only helpers for isolating package test
// binaries inside owned PTYs so terminal state changes cannot leak to the
// caller's controlling terminal.
package testpty

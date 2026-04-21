//go:build nolblogs

package main

import "context"

// startLBLogs is a no-op stub compiled when the nolblogs build tag is set.
// This excludes the AWS SDK from the binary, reducing size by ~7 MB.
func startLBLogs(_ context.Context, _ Config) {}

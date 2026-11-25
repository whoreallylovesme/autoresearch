package main

import "testing"

func TestDaemonFlagsKeepNetworkBoundaryExplicit(t *testing.T) {
	root := newRootCommand()
	for _, name := range []string{"config", "listen", "data-dir", "allow-non-loopback"} {
		if root.Flag(name) == nil {
			t.Fatalf("daemon flag %q is missing", name)
		}
	}
}

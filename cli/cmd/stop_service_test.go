package main

import (
	"testing"

	"mindfs/server/app"
)

// The graceful-stop API attempt must report failure (so stopService falls
// back to the platform kill path) when no token exists or nothing is
// listening — never hang or false-positive.
func TestRequestShutdownViaAPIFailsClosedWithoutServer(t *testing.T) {
	confDir := t.TempDir()
	t.Setenv("APPDATA", confDir)
	t.Setenv("HOME", confDir)
	t.Setenv("XDG_CONFIG_HOME", confDir)

	if requestShutdownViaAPI("127.0.0.1:1", false) {
		t.Fatal("succeeded with no stored token")
	}

	if _, err := app.EnsureLocalCLIToken("127.0.0.1:1"); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if requestShutdownViaAPI("127.0.0.1:1", false) {
		t.Fatal("succeeded with a token but no listening server")
	}
}

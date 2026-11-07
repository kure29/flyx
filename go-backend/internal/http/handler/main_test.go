package handler

import (
	"os"
	"testing"
)

// Tests talk to fake GitHub servers; a GITHUB_TOKEN from the environment
// would switch release downloads to the authenticated API path.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("GITHUB_TOKEN")
	os.Exit(m.Run())
}

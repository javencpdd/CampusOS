package projectaudit

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A plugin installation must not require a publisher/key exception in the
// compiled platform. Exercise generic behavior separately in UI action tests.
func TestRSSHasNoIdentityExceptionOrParserInTheHost(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"web/src", "internal/plugin", "internal/server", "internal/transport/httpapi"} {
		err = filepath.WalkDir(filepath.Join(root, scope), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"campusos.rss-publisher", "internal/platform/rssreader", "pkg/syndication", "RSSReaderToolbar", "rssPluginHost"} {
				if strings.Contains(string(body), forbidden) {
					t.Errorf("plugin business exception in host %s: %s", path, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreAvailable(t *testing.T) {
	names, err := fs.Glob(Files, "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no forward migrations embedded in the binary")
	}
	for _, name := range names {
		data, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("embedded migration %s is empty", name)
		}
	}
}

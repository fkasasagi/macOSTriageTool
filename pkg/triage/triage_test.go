package triage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/conf"
	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/log"
)

// A copy unit that is itself a symlink must be copied as a link: ditto would
// follow it and pull in host files (or files from an excluded tree).
// Many units sharing a parent must all arrive under -j > 1.
func TestCopyAllSymlinkUnitsAndParallelParents(t *testing.T) {
	logDir := t.TempDir()
	if err := log.Init(logDir); err != nil {
		t.Fatal(err)
	}
	root, host, out := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(host, "hostfile"), []byte("hostsecret"), 0o644)
	user := filepath.Join(root, "Users/alice")
	os.MkdirAll(user, 0o755)
	os.Symlink(filepath.Join(host, "hostfile"), filepath.Join(user, ".zsh_history"))
	os.Symlink(host, filepath.Join(user, "Downloads"))
	var units []string
	for i := 0; i < 40; i++ {
		name := filepath.Join("/Users/alice", "f"+string(rune('a'+i%26))+string(rune('a'+i/26)))
		os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644)
		units = append(units, name)
	}
	units = append(units, "/Users/alice/.zsh_history", "/Users/alice/Downloads")

	cfg := &conf.Config{RootPath: root, OutputDirPath: out, Jobs: 8}
	copyAll(units, cfg)

	for _, l := range []string{".zsh_history", "Downloads"} {
		fi, err := os.Lstat(filepath.Join(out, "Users/alice", l))
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s must be copied as a symlink (err=%v)", l, err)
		}
	}
	for _, u := range units[:40] {
		if _, err := os.Stat(filepath.Join(out, u)); err != nil {
			t.Errorf("missing after parallel copy: %s", u)
		}
	}
}

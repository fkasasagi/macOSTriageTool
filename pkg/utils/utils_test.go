package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/log"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "mtt-log")
	if err := log.Init(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func mustExcluder(t *testing.T, p ...string) *Excluder {
	t.Helper()
	ex, err := NewExcluder(p)
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func TestExcluder(t *testing.T) {
	ex := mustExcluder(t, "/Users/*/Documents")
	cases := []struct {
		path       string
		excluded   bool
		descendant bool
	}{
		{"/Users/alice/Documents", true, false},
		{"/Users/alice/Documents/secret.txt", true, false},
		{"/Users/alice/Documents2", false, false},
		{"/Users/alice", false, true},
		{"/Users", false, true},
		{"/", false, true},
		{"/Users/alice/Library", false, false},
		{"/Users/alice/Library/Containers/com.apple.Maps/Data/Documents", false, false},
	}
	for _, c := range cases {
		if got := ex.Excluded(c.path); got != c.excluded {
			t.Errorf("Excluded(%q) = %v, want %v", c.path, got, c.excluded)
		}
		if got := ex.HasExcludedDescendant(c.path); got != c.descendant {
			t.Errorf("HasExcludedDescendant(%q) = %v, want %v", c.path, got, c.descendant)
		}
	}
	if _, err := NewExcluder([]string{"Users/*/Documents"}); err == nil {
		t.Error("relative pattern must be rejected")
	}
	var nilEx *Excluder
	if nilEx.Excluded("/x") || nilEx.HasExcludedDescendant("/x") {
		t.Error("nil excluder must exclude nothing")
	}
}

func TestGuestCandidates(t *testing.T) {
	cases := map[string][]string{
		"/System/Volumes/Data/.fseventsd/": {"/System/Volumes/Data/.fseventsd/", "/.fseventsd/"},
		"/var/root/.zsh_history":           {"/var/root/.zsh_history", "/private/var/root/.zsh_history"},
		"/tmp/":                            {"/tmp/", "/private/tmp/"},
		"/tmp":                             {"/tmp", "/private/tmp"},
		"/Users/*/Library/":                {"/Users/*/Library/"},
	}
	for in, want := range cases {
		if got := guestCandidates(in); !reflect.DeepEqual(got, want) {
			t.Errorf("guestCandidates(%q) = %v, want %v", in, got, want)
		}
	}
}

// fakeRoot builds a small Data-volume-like tree and returns its path.
func fakeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{
		"Users/alice/Documents/target.txt",
		"Users/alice/.zsh_history",
		"Users/alice/Library/Preferences/a.plist",
		"private/var/root/.zsh_history",
		".fseventsd/0000000000000001",
	}
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveGuestPaths(t *testing.T) {
	root := fakeRoot(t)
	got := ResolveGuestPaths(root, "/System/Volumes/Data/.fseventsd/", nil)
	if !reflect.DeepEqual(got, []string{"/.fseventsd"}) {
		t.Errorf("fseventsd alias: %v", got)
	}
	got = ResolveGuestPaths(root, "/var/root/.*sh_history", nil)
	if !reflect.DeepEqual(got, []string{"/private/var/root/.zsh_history"}) {
		t.Errorf("var alias: %v", got)
	}
	if got := ResolveGuestPaths(root, "/nonexistent", nil); got != nil {
		t.Errorf("nonexistent: %v", got)
	}
}

func TestResolveGuestPathsDoesNotEscapeRoot(t *testing.T) {
	root := fakeRoot(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "hostfile"), []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	// guest symlink pointing at a host directory
	if err := os.Symlink(outside, filepath.Join(root, "Users/alice/Library/Evil")); err != nil {
		t.Fatal(err)
	}
	if got := ResolveGuestPaths(root, "/Users/*/Library/Evil/hostfile", nil); got != nil {
		t.Errorf("escaped root via symlink: %v", got)
	}
	// the link itself may be collected (copied as a link)
	if got := ResolveGuestPaths(root, "/Users/*/Library/Evil", nil); !reflect.DeepEqual(got, []string{"/Users/alice/Library/Evil"}) {
		t.Errorf("link itself: %v", got)
	}
}

func TestCopyUnitsSplitsAroundExcludesAndDropsNested(t *testing.T) {
	root := fakeRoot(t)
	ex := mustExcluder(t, "/Users/*/Documents")
	got := CopyUnits(root, []string{"/Users/alice", "/Users/alice/Library/Preferences", "/Users/alice/Documents/target.txt"}, ex)
	want := []string{"/Users/alice/.zsh_history", "/Users/alice/Library"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CopyUnits = %v, want %v", got, want)
	}
}

func TestBodyfileListsEverything(t *testing.T) {
	root := fakeRoot(t)
	var buf bytes.Buffer
	n, err := WriteBodyfile(&buf, root)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "|/Users/alice/Documents/target.txt|") {
		t.Errorf("bodyfile must list every file, excludes are not applied:\n%s", out)
	}
	if strings.Contains(out, root) {
		t.Errorf("host root path leaked into bodyfile:\n%s", out)
	}
	if lines := strings.Count(out, "\n"); lines != n {
		t.Errorf("count %d != lines %d", n, lines)
	}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Split(l, "|"); len(f) != 11 || f[0] != "0" {
			t.Errorf("bad bodyfile line: %q", l)
		}
	}
}

func TestCheckOutputOutsideRoot(t *testing.T) {
	if err := CheckOutputOutsideRoot("/mnt/data", "/mnt/data/out"); err == nil {
		t.Error("output inside root must be rejected")
	}
	if err := CheckOutputOutsideRoot("/mnt/data", "/mnt/database"); err != nil {
		t.Error(err)
	}
	if err := CheckOutputOutsideRoot("/", "/tmp/out"); err != nil {
		t.Error(err)
	}
}

func TestResolveGuestPathsLiveRoot(t *testing.T) {
	// "/" as root (live mode) must still resolve nested paths
	if got := ResolveGuestPaths("/", "/private/etc/hosts", nil); !reflect.DeepEqual(got, []string{"/private/etc/hosts"}) {
		t.Errorf("live root: %v", got)
	}
}

func TestResolveGuestPathsHonoursExcludeThroughSymlinkedParent(t *testing.T) {
	root := fakeRoot(t)
	// Library/Leak -> ../Documents : entry looks harmless, really lives in Documents
	if err := os.Symlink("../Documents", filepath.Join(root, "Users/alice/Library/Leak")); err != nil {
		t.Fatal(err)
	}
	ex := mustExcluder(t, "/Users/*/Documents")
	if got := ResolveGuestPaths(root, "/Users/*/Library/Leak/target.txt", ex); got != nil {
		t.Errorf("exclude bypassed via symlinked parent: %v", got)
	}
}

func TestExcluderCaseAndAliases(t *testing.T) {
	ex := mustExcluder(t, "/users/*/documents")
	if !ex.Excluded("/Users/alice/Documents/x") || !ex.Excluded("/Users/alice/DOCUMENTS") {
		t.Error("matching must be case-insensitive")
	}
	ex = mustExcluder(t, "/System/Volumes/Data/Users/*/Documents")
	if !ex.Excluded("/Users/alice/Documents") {
		t.Error("alias form of pattern must also exclude the Data-volume path")
	}
}

func TestDropNestedWithInterleavedSiblings(t *testing.T) {
	got := dropNested([]string{"/private/var/log", "/private/var/log.old", "/private/var/log/asl"})
	want := []string{"/private/var/log", "/private/var/log.old"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dropNested = %v, want %v", got, want)
	}
}

func TestModeStringSpecialBits(t *testing.T) {
	if got := modeString(os.ModeDir | os.ModeSticky | 0o777); got != "drwxrwxrwt" {
		t.Errorf("sticky: %s", got)
	}
	if got := modeString(os.ModeSetuid | 0o644); got != "-rwSr--r--" {
		t.Errorf("setuid w/o x: %s", got)
	}
}

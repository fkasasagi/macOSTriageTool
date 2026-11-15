package utils

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A guest path is an absolute path as seen inside the evidence system
// ("/Users/alice/..."). The host path is RootPath + guest path.

// When the root is a mounted Data volume (offline acquisition), the live-system
// paths in the preset list do not exist verbatim. These prefixes are tried as
// fallbacks, in order.
var dataVolumeAliases = [][2]string{
	{"/System/Volumes/Data/", "/"},
	{"/var/", "/private/var/"},
	{"/tmp/", "/private/tmp/"},
	{"/etc/", "/private/etc/"},
}

func guestCandidates(guest string) []string {
	cands := []string{guest}
	probe := guest
	if !strings.HasSuffix(probe, "/") {
		probe += "/"
	}
	for _, a := range dataVolumeAliases {
		if strings.HasPrefix(probe, a[0]) {
			alt := a[1] + strings.TrimPrefix(probe, a[0])
			if !strings.HasSuffix(guest, "/") {
				alt = strings.TrimSuffix(alt, "/")
			}
			cands = append(cands, alt)
		}
	}
	return cands
}

// ResolveGuestPaths expands one preset/custom entry into existing guest paths
// under root. It tries the entry literally first, then its Data-volume aliases.
// Matches whose real location escapes root (via symlinks) are dropped so that
// host files are never collected.
func ResolveGuestPaths(root, entry string, ex *Excluder) []string {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil
	}
	for _, cand := range guestCandidates(entry) {
		hostPattern := filepath.Join(root, cand)
		matches, err := filepath.Glob(hostPattern)
		if err != nil || len(matches) == 0 {
			continue
		}
		var out []string
		for _, m := range matches {
			realGuest, ok := realGuestPath(realRoot, m)
			if !ok || ex.Excluded(realGuest) {
				continue // escapes root, or really lives in an excluded tree
			}
			out = append(out, toGuest(root, m))
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// realGuestPath resolves the parent directory of hostPath (the final component
// is copied as a link, never followed) and returns its real guest path. ok is
// false when the real location is outside realRoot.
func realGuestPath(realRoot, hostPath string) (string, bool) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(hostPath))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realRoot, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return toGuest(realRoot, filepath.Join(parent, filepath.Base(hostPath))), true
}

func toGuest(root, hostPath string) string {
	rel, err := filepath.Rel(root, hostPath)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

// CopyUnits turns resolved guest paths into the list of independent copy units:
// excluded paths are removed, directories containing an excluded path are split
// into their children, and paths nested under another unit are dropped (so no
// two parallel copies write the same destination).
func CopyUnits(root string, guests []string, ex *Excluder) []string {
	var units []string
	for _, g := range guests {
		units = append(units, splitAroundExcludes(root, filepath.Clean(g), ex)...)
	}
	return dropNested(units)
}

func splitAroundExcludes(root, guest string, ex *Excluder) []string {
	if ex.Excluded(guest) {
		return nil
	}
	if !ex.HasExcludedDescendant(guest) {
		return []string{guest}
	}
	host := filepath.Join(root, guest)
	info, err := os.Lstat(host)
	if err != nil || !info.IsDir() {
		return []string{guest}
	}
	entries, err := os.ReadDir(host)
	if err != nil {
		return []string{guest}
	}
	var out []string
	for _, e := range entries {
		out = append(out, splitAroundExcludes(root, filepath.Join(guest, e.Name()), ex)...)
	}
	return out
}

func dropNested(paths []string) []string {
	kept := map[string]bool{}
	var out []string
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	for _, p := range sorted {
		if hasKeptAncestorOrSelf(kept, p) {
			continue
		}
		kept[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func hasKeptAncestorOrSelf(kept map[string]bool, p string) bool {
	for q := p; ; q = filepath.Dir(q) {
		if kept[q] {
			return true
		}
		if q == "/" || q == "." {
			return false
		}
	}
}

// CheckOutputOutsideRoot refuses an output location inside the evidence root.
func CheckOutputOutsideRoot(root, output string) error {
	r, err1 := filepath.Abs(root)
	o, err2 := filepath.Abs(output)
	if err1 != nil || err2 != nil {
		return errors.New("[-] Failed to resolve root/output path")
	}
	if r == "/" {
		return nil // live acquisition: output on the same system is the normal case
	}
	if o == r || strings.HasPrefix(o, r+string(filepath.Separator)) {
		return errors.New("[-] Output path must not be inside the root path")
	}
	return nil
}

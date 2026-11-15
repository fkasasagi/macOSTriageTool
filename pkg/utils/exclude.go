package utils

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Excluder holds guest-path glob patterns ("/Users/*/Documents"). A path is
// excluded when it, or one of its ancestors, matches a pattern component-wise,
// case-insensitively. Data-volume alias forms of a pattern are matched too.
type Excluder struct {
	patterns [][]string
}

func NewExcluder(patterns []string) (*Excluder, error) {
	ex := &Excluder{}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("[-] Exclude pattern must be an absolute guest path: %q", p)
		}
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, fmt.Errorf("[-] Bad exclude pattern %q: %v", p, err)
		}
		for _, c := range guestCandidates(p) {
			ex.patterns = append(ex.patterns, split(strings.ToLower(c)))
		}
	}
	return ex, nil
}

func split(p string) []string {
	p = strings.Trim(filepath.Clean(p), "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// prefixMatches reports whether the first len(path) components of pat match path.
// pat is already lower-case; APFS is case-insensitive by default, so is matching.
func prefixMatches(pat, path []string) bool {
	for i := range path {
		if ok, _ := filepath.Match(pat[i], strings.ToLower(path[i])); !ok {
			return false
		}
	}
	return true
}

func (e *Excluder) Excluded(guest string) bool {
	if e == nil {
		return false
	}
	path := split(guest)
	for _, pat := range e.patterns {
		if len(pat) <= len(path) && prefixMatches(pat, path[:len(pat)]) {
			return true
		}
	}
	return false
}

// HasExcludedDescendant reports whether some path below guest could be excluded.
func (e *Excluder) HasExcludedDescendant(guest string) bool {
	if e == nil {
		return false
	}
	path := split(guest)
	for _, pat := range e.patterns {
		if len(pat) > len(path) && prefixMatches(pat, path) {
			return true
		}
	}
	return false
}

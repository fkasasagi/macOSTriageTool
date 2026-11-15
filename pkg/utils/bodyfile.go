package utils

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/log"
)

// WriteBodyfile walks every file under root (symlinks are not followed) and
// writes one TSK 3.x bodyfile line per entry, skipping excluded subtrees.
// Unreadable entries are logged and skipped.
func WriteBodyfile(w io.Writer, root string, ex *Excluder) (int, error) {
	bw := bufio.NewWriterSize(w, 1<<20)
	n := 0
	err := filepath.WalkDir(root, func(host string, d fs.DirEntry, err error) error {
		guest := toGuest(root, host)
		if err != nil {
			log.FileInfoErrorLogger.Error("[-] bodyfile: " + guest + ": " + err.Error())
			return nil
		}
		if ex.Excluded(guest) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(host)
		if err != nil {
			log.FileInfoErrorLogger.Error("[-] bodyfile: " + guest + ": " + err.Error())
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		name := guest
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(host); err == nil {
				name += " -> " + target
			}
		}
		if _, err := bw.WriteString(BodyLine(name, info.Mode(), st) + "\n"); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	return n, bw.Flush()
}

// BodyLine formats MD5|name|inode|mode_as_string|UID|GID|size|atime|mtime|ctime|crtime
// with times as Unix epoch seconds (UTC), as consumed by mactime.
func BodyLine(name string, mode os.FileMode, st *syscall.Stat_t) string {
	name = strings.NewReplacer("|", "\\|", "\n", "\\n").Replace(name)
	return fmt.Sprintf("0|%s|%d|%s|%d|%d|%d|%d|%d|%d|%d",
		name, st.Ino, modeString(mode), st.Uid, st.Gid, st.Size,
		st.Atimespec.Sec, st.Mtimespec.Sec, st.Ctimespec.Sec, st.Birthtimespec.Sec)
}

// modeString renders the type letter the way TSK/ls do ("d", "l", "-") followed
// by the permission bits; Go's FileMode.String uses "L" for symlinks.
func modeString(m os.FileMode) string {
	t := "-"
	switch {
	case m.IsDir():
		t = "d"
	case m&os.ModeSymlink != 0:
		t = "l"
	case m&os.ModeNamedPipe != 0:
		t = "p"
	case m&os.ModeSocket != 0:
		t = "s"
	case m&os.ModeDevice != 0 && m&os.ModeCharDevice != 0:
		t = "c"
	case m&os.ModeDevice != 0:
		t = "b"
	}
	perm := []byte(m.Perm().String()[1:])
	if m&os.ModeSetuid != 0 {
		perm[2] = special(perm[2], 's')
	}
	if m&os.ModeSetgid != 0 {
		perm[5] = special(perm[5], 's')
	}
	if m&os.ModeSticky != 0 {
		perm[8] = special(perm[8], 't')
	}
	return t + string(perm)
}

func special(x byte, c byte) byte {
	if x == 'x' {
		return c
	}
	return c - ('a' - 'A') // no execute bit: S / T
}

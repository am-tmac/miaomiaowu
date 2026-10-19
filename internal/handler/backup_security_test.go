package handler

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type backupSecurityEntry struct {
	name string
	mode os.FileMode
	body string
}

func writeSecurityBackup(t *testing.T, entries ...backupSecurityEntry) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name}
		h.SetMode(e.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestExtractBackupRejectsArchiveSpecialEntries(t *testing.T) {
	for _, mode := range []os.FileMode{os.ModeSymlink | 0777, os.ModeNamedPipe | 0600, os.ModeSocket | 0600, os.ModeDevice | 0600} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Chdir(t.TempDir())
			archive := writeSecurityBackup(t, backupSecurityEntry{"data/special", mode, "outside"})
			if err := extractBackup(archive); err == nil {
				info, statErr := os.Lstat("data/special")
				if statErr == nil {
					t.Logf("original extractor materializes archive mode %v as mode %v (not an actual symlink)", mode, info.Mode())
				}
				t.Fatal("accepted special archive entry")
			}
			if _, err := os.Lstat("data/special"); !os.IsNotExist(err) {
				t.Fatalf("special entry was written: %v", err)
			}
		})
	}
}

func TestExtractBackupConstrainedToDataRoots(t *testing.T) {
	for _, kind := range []string{"parent", "target", "root", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())
			outside := t.TempDir()
			target := filepath.Join(outside, "victim")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir("data", 0755); err != nil {
				t.Fatal(err)
			}
			name := "data/link/victim"
			switch kind {
			case "parent":
				if err := os.Symlink(outside, "data/link"); err != nil {
					t.Fatal(err)
				}
			case "target":
				name = "data/victim"
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			case "root":
				os.Remove("data")
				if err := os.Symlink(outside, "data"); err != nil {
					t.Fatal(err)
				}
				name = "data/victim"
			case "sibling":
				if err := os.Mkdir("subscribes", 0755); err != nil {
					t.Fatal(err)
				}
				target = "subscribes/victim"
				if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../subscribes", "data/link"); err != nil {
					t.Fatal(err)
				}
			}
			archive := writeSecurityBackup(t, backupSecurityEntry{name, 0600, "overwritten"})
			err := extractBackup(archive)
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "original" {
				t.Errorf("escaped write changed outside target to %q", got)
			}
			if err == nil {
				t.Error("accepted existing symlink escape")
			}
		})
	}
}

func TestExtractBackupPreservesRootAndExistingFileIdentity(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/db", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeRoot, err := os.Stat("data")
	if err != nil {
		t.Fatal(err)
	}
	beforeFile, err := os.Stat("data/db")
	if err != nil {
		t.Fatal(err)
	}
	archive := writeSecurityBackup(t,
		backupSecurityEntry{"data/", os.ModeDir | 0755, ""},
		backupSecurityEntry{"data/db", 0600, "restored"},
		backupSecurityEntry{"subscribes/nested/file", 0600, "subscription"},
		backupSecurityEntry{"data/../../escape", 0600, "bad"},
		backupSecurityEntry{"unrelated/file", 0600, "bad"},
	)
	if err := extractBackup(archive); err != nil {
		t.Fatal(err)
	}
	afterRoot, err := os.Stat("data")
	if err != nil {
		t.Fatal(err)
	}
	afterFile, err := os.Stat("data/db")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeRoot, afterRoot) || !os.SameFile(beforeFile, afterFile) {
		t.Fatal("restore replaced live data root or database inode")
	}
	for name, want := range map[string]string{"data/db": "restored", "subscribes/nested/file": "subscription"} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, error %v", name, got, err)
		}
	}
	if _, err := os.Stat("unrelated"); !os.IsNotExist(err) {
		t.Fatalf("unexpected unrelated entry: %v", err)
	}
}

func TestBackupSkipsSymlinks(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("sensitive external bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, "data/link"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/normal", []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := addDirToZip(zw, "data", "data"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "data/normal" {
		t.Fatalf("backup must contain only regular files, got %v", zr.File)
	}
}

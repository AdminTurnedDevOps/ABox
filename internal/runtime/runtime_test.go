package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/guestimage"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func writeTestImage(t *testing.T, protocolVersion int, arch string, corruptDigest bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "guest.raw")
	if err := os.WriteFile(path, []byte("GOLDEN"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := guestimage.Digest(path)
	if err != nil {
		t.Fatal(err)
	}
	if corruptDigest {
		digest = strings.Repeat("0", 64)
	}
	manifest := guestimage.Manifest{Schema: guestimage.Schema, Arch: arch, ImageID: "test-image", Protocol: protocolVersion, SHA256: digest}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".manifest.json", data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func compatibleSessionMetadata(t *testing.T, s *session.Session) {
	t.Helper()
	backend, err := hostVMMBackend()
	if err != nil {
		t.Fatal(err)
	}
	s.GuestArch = goruntime.GOARCH
	s.ManifestSchema = guestimage.Schema
	s.ImageID = "test-image"
	s.ImageSHA256 = strings.Repeat("a", 64)
	s.GuestProtocol = protocol.Version
	s.VMMBackend = backend
	if err := s.WriteMeta(); err != nil {
		t.Fatal(err)
	}
}

func TestCloneFileCopiesContents(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello-abox"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello-abox" {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestCloneFileRemovesPartialDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source-dir")
	dst := filepath.Join(dir, "dst")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, dst); err == nil {
		t.Fatal("cloneFile succeeded for a directory")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial destination remains: %v", err)
	}
}

func TestPrepareResumeDoesNotClobberRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("session-disk-bytes")
	if err := os.WriteFile(s.RootDisk(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	compatibleSessionMetadata(t, s)
	golden := filepath.Join(t.TempDir(), "golden.raw")
	err = Prepare(s, golden, config.Model{Name: "grok", Provider: "xai", Model: "grok-4"}, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(s.RootDisk())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("root.raw clobbered: %q", got)
	}
	if _, err := os.Stat(s.ConfigDisk()); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareResumeRewritesReadOnlyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ConfigDisk(), make([]byte, 1<<20), 0o400); err != nil {
		t.Fatal(err)
	}
	compatibleSessionMetadata(t, s)
	err = Prepare(s, "", config.Model{Name: "grok", Provider: "xai", Model: "grok-4"}, true)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(s.ConfigDisk())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o400 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
}

func TestPreparePersistsVerifiedImageMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	image := writeTestImage(t, protocol.Version, goruntime.GOARCH, false)
	if err := Prepare(s, image, config.Model{Name: "grok"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := session.Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ManifestSchema != guestimage.Schema || loaded.GuestArch != goruntime.GOARCH || loaded.ImageID != "test-image" || loaded.GuestProtocol != protocol.Version {
		t.Fatalf("metadata = %#v", loaded)
	}
	digest, err := guestimage.Digest(loaded.RootDisk())
	if err != nil {
		t.Fatal(err)
	}
	if digest != loaded.ImageSHA256 {
		t.Fatalf("root digest %s, metadata %s", digest, loaded.ImageSHA256)
	}
}

func TestPrepareRejectsImageArchitecture(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	other := "arm64"
	if goruntime.GOARCH == other {
		other = "amd64"
	}
	err = Prepare(s, writeTestImage(t, protocol.Version, other, false), config.Model{}, false)
	if err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("got %v", err)
	}
}

func TestPrepareRejectsDigestMismatchAndRemovesClone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	err = Prepare(s, writeTestImage(t, protocol.Version, goruntime.GOARCH, true), config.Model{}, false)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(s.RootDisk()); !os.IsNotExist(err) {
		t.Fatalf("failed clone remains: %v", err)
	}
}

func TestPrepareProtocolPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		probe    bool
		wantErr  bool
		contains string
	}{
		{name: "normal older", version: protocol.Version - 1, wantErr: true, contains: "incompatible"},
		{name: "probe older", version: protocol.Version - 1, probe: true},
		{name: "future", version: protocol.Version + 1, probe: true, wantErr: true, contains: "newer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, err := session.Create("/source")
			if err != nil {
				t.Fatal(err)
			}
			image := writeTestImage(t, tc.version, goruntime.GOARCH, false)
			if tc.probe {
				err = PrepareProbe(s, image, config.Model{})
			} else {
				err = Prepare(s, image, config.Model{}, false)
			}
			if tc.wantErr && (err == nil || !strings.Contains(err.Error(), tc.contains)) {
				t.Fatalf("got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrepareResumeRejectsMetadataFreeLinuxSession(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("Linux compatibility rule")
	}
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Prepare(s, "", config.Model{}, true)
	if err == nil || !strings.Contains(err.Error(), "predates image compatibility metadata") {
		t.Fatalf("got %v", err)
	}
}

func TestPrepareResumeRejectsPartialLinuxMetadata(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("Linux compatibility rule")
	}
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.GuestArch = goruntime.GOARCH
	s.GuestProtocol = protocol.Version
	s.VMMBackend = "kvm"
	if err := s.WriteMeta(); err != nil {
		t.Fatal(err)
	}
	err = Prepare(s, "", config.Model{}, true)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("got %v", err)
	}
}

func TestMapHelperDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"symbol lookup error: undefined symbol: krun_disable_implicit_vsock", "incompatible libkrun package or ABI"},
		{"error while loading shared libraries: libkrun.so.1: cannot open shared object file", "libkrun is linked but not loadable"},
		{"libkrunfw.so.5 not found", "firmware library required"},
	} {
		if got := mapHelperDiagnostic(tc.input); !strings.Contains(got, tc.want) {
			t.Fatalf("mapHelperDiagnostic(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

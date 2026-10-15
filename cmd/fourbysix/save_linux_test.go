package main

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/zzwong/fourbysix/internal/pdfdoc"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

// A real kernel write failure exercises unchanged production code as a red
// control. Limit only the isolated child; never depend on free disk space.
func TestRunSaveKernelFailurePreservesDestination(t *testing.T) {
	if os.Getenv("FOURBYSIX_SAVE_LIMIT_CHILD") == "1" {
		// Warm PDFium before limiting output writes, so compilation-cache writes
		// cannot be mistaken for the save failure.
		data, err := os.ReadFile(os.Getenv("FOURBYSIX_SAVE_INPUT"))
		if err != nil {
			os.Exit(91)
		}
		doc, err := pdfdoc.Open(data)
		if err != nil {
			os.Exit(92)
		}
		doc.Close()
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 64, Max: 64}); err != nil {
			os.Exit(90)
		}
		os.Exit(run([]string{os.Getenv("FOURBYSIX_SAVE_INPUT"), "-o", os.Getenv("FOURBYSIX_SAVE_OUTPUT"), "--print"}, os.Stdout, os.Stderr))
	}
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{true: "existing", false: "absent"}[exists], func(t *testing.T) {
			dir := t.TempDir()
			capture := installFakeLP(t)
			src := writeFixture(t, dir, []testpdf.Page{{}})
			out := filepath.Join(dir, "out.pdf")
			var old []byte
			if exists {
				old = []byte("previous output must remain byte-for-byte")
				if err := os.WriteFile(out, old, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestRunSaveKernelFailurePreservesDestination$")
			cmd.Env = append(os.Environ(), "FOURBYSIX_SAVE_LIMIT_CHILD=1", "FOURBYSIX_SAVE_INPUT="+src, "FOURBYSIX_SAVE_OUTPUT="+out, "XDG_CONFIG_HOME="+dir)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || !strings.Contains(stderr.String(), "file too large") {
				t.Fatalf("error=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "Saved:") {
				t.Fatalf("false success: %q", stdout.String())
			}
			assertLPNotCalled(t, capture)
			got, err := os.ReadFile(out)
			if exists {
				if err != nil || !bytes.Equal(got, old) {
					t.Fatalf("existing destination changed: %q error=%v", got, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("absent destination created: %q error=%v", got, err)
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".fourbysix-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("leftovers=%v error=%v", matches, err)
			}
		})
	}
}

func TestSaveRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	err := saveFile(path, nil)
	if err == nil {
		t.Fatal("FIFO accepted")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO changed: %v", err)
	}
}

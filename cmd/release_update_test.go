package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(data)
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func TestSemverNewer(t *testing.T) {
	for _, tc := range []struct {
		have, latest string
		want         bool
	}{
		{"0.1.0", "v0.2.0", true}, {"v0.2.0", "v0.2.0", false}, {"0.10.0", "v0.9.0", false},
		{"dev", "v1.0.0", false}, {"", "v1.0.0", true}, {"1.2.3", "garbage", false},
	} {
		if got := semverNewer(tc.have, tc.latest); got != tc.want {
			t.Errorf("semverNewer(%q,%q) = %v, want %v", tc.have, tc.latest, got, tc.want)
		}
	}
}

func TestUpdateFromReleaseVerifiesChecksumAndInstalls(t *testing.T) {
	archive := fmt.Sprintf("democtl_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	payload := []byte("#!/bin/sh\necho democtl version 1.2.0\n")
	tgz := tarGz(t, "democtl", payload)
	sum := sha256.Sum256(tgz)
	good := hex.EncodeToString(sum[:]) + "  " + archive + "\n"

	var checksums string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/aeon022/democtl/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v1.2.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
			archive, srv.URL+"/a.tgz", srv.URL+"/checksums.txt")
	})
	mux.HandleFunc("/a.tgz", func(w http.ResponseWriter, r *http.Request) { w.Write(tgz) })
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, checksums) })

	old := releaseAPI
	releaseAPI = srv.URL
	t.Cleanup(func() { releaseAPI = old })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin") // democtl is not installed anywhere yet
	target := filepath.Join(home, ".local", "bin", "democtl")

	// a wrong checksum must install nothing
	checksums = "deadbeef  " + archive + "\n"
	if _, err := updateFromRelease(context.Background(), srv.Client(), "democtl", false); err == nil {
		t.Fatal("a checksum mismatch must fail")
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("nothing may be installed after a checksum mismatch")
	}

	// --check reports without installing
	checksums = good
	msg, err := updateFromRelease(context.Background(), srv.Client(), "democtl", true)
	if err != nil || msg == "" {
		t.Fatalf("check: %q %v", msg, err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("--check must not install")
	}

	// a good release installs an executable binary
	if _, err := updateFromRelease(context.Background(), srv.Client(), "democtl", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(b, payload) {
		t.Fatalf("binary not installed correctly: %v", err)
	}
	if fi, _ := os.Stat(target); fi.Mode()&0o111 == 0 {
		t.Error("the installed binary must be executable")
	}

	// installed == latest → up to date
	if msg, _ := updateFromRelease(context.Background(), srv.Client(), "democtl", false); msg != "up to date (1.2.0)" {
		t.Errorf("second run: %q", msg)
	}
}

func TestNoReleaseYetIsAFriendlyError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := releaseAPI
	releaseAPI = srv.URL
	t.Cleanup(func() { releaseAPI = old })
	t.Setenv("HOME", t.TempDir())
	if _, err := updateFromRelease(context.Background(), srv.Client(), "nopectl", false); err == nil || err.Error() != "no release published yet" {
		t.Errorf("got %v", err)
	}
}

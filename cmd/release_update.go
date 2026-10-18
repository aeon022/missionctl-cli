package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Binary updates: for a tool without a source checkout, `missionctl update` fetches the latest GitHub release
// (the archives goreleaser publishes: <tool>_<version>_<os>_<arch>.tar.gz plus checksums.txt), verifies the
// SHA-256 and replaces the installed binary. Builds from source report "dev" and are never overwritten.

var releaseAPI = "https://api.github.com" // overridden by tests

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Assets []ghAsset `json:"assets"`
}

func latestRelease(ctx context.Context, c *http.Client, repo string) (*ghRelease, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI+"/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no release published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// pickAssets finds the tarball for this platform and the checksums file.
func pickAssets(rel *ghRelease, tool, goos, goarch string) (archive, sums ghAsset, ok bool) {
	suffix := fmt.Sprintf("_%s_%s.tar.gz", goos, goarch)
	for _, a := range rel.Assets {
		switch {
		case strings.HasPrefix(a.Name, tool+"_") && strings.HasSuffix(a.Name, suffix):
			archive = a
		case a.Name == "checksums.txt":
			sums = a
		}
	}
	return archive, sums, archive.Name != "" && sums.Name != ""
}

// checksumFor reads "<sha256>  <filename>" lines (sha256sum format) from checksums.txt.
func checksumFor(checksums []byte, filename string) (string, bool) {
	for _, line := range strings.Split(string(checksums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == filename {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extractBinary returns the file called name from a .tar.gz.
func extractBinary(tgz []byte, name string) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

// installBinary writes data next to the target and renames it into place, so a running copy keeps working.
func installBinary(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var versionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// semverNewer reports whether latest is a higher x.y.z than installed. "dev" (a source build) is never updated.
func semverNewer(installed, latest string) bool {
	a, b := versionRe.FindStringSubmatch(installed), versionRe.FindStringSubmatch(latest)
	if b == nil {
		return false
	}
	if a == nil { // unknown output other than "dev": offer the release
		return !strings.Contains(installed, "dev")
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if y != x {
			return y > x
		}
	}
	return false
}

// installedVersion runs `<bin> --version` ("habctl version 0.2.1" / "habctl version dev").
func installedVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// installTargetFor is where a tool's binary lives: where it is on PATH, else ~/.local/bin.
func installTargetFor(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return filepath.Join(expandHome("~/.local/bin"), name)
}

func httpGet(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// updateFromRelease checks (and, unless checkOnly, installs) the latest release of one tool.
// It returns a one-line status for the update table.
func updateFromRelease(ctx context.Context, c *http.Client, name string, checkOnly bool) (string, error) {
	target := installTargetFor(name)
	have := ""
	if _, err := os.Stat(target); err == nil {
		have = installedVersion(target)
	}
	if have == "dev" {
		return "built from source (dev) — left alone", nil
	}
	rel, err := latestRelease(ctx, c, "aeon022/"+name)
	if err != nil {
		return "", err
	}
	if have != "" && !semverNewer(have, rel.Tag) {
		return "up to date (" + have + ")", nil
	}
	if checkOnly {
		return fmt.Sprintf("update available: %s → %s", orDash(have), rel.Tag), nil
	}
	arch, sums, ok := pickAssets(rel, name, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return "", fmt.Errorf("release %s has no %s/%s build", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	tgz, err := httpGet(ctx, c, arch.URL, 200<<20)
	if err != nil {
		return "", err
	}
	sumFile, err := httpGet(ctx, c, sums.URL, 1<<20)
	if err != nil {
		return "", err
	}
	want, found := checksumFor(sumFile, arch.Name)
	got := sha256.Sum256(tgz)
	if !found || hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("checksum mismatch for %s — nothing was installed", arch.Name)
	}
	bin, err := extractBinary(tgz, name)
	if err != nil {
		return "", err
	}
	if err := installBinary(target, bin); err != nil {
		return "", err
	}
	return fmt.Sprintf("installed %s → %s", rel.Tag, target), nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func newReleaseClient() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const DefaultRepo = "ARTJ1/keenetic-mtproto-main"

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type CheckResult struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	UpdateAvail bool   `json:"update_available"`
	Asset       string `json:"asset,omitempty"`
	URL         string `json:"url,omitempty"`
	Arch        string `json:"arch"`
	Repo        string `json:"repo"`
}

func ArchSuffix() string {
	switch runtime.GOARCH {
	case "amd64":
		return "linux-amd64"
	case "arm64":
		return "linux-arm64"
	case "arm":
		return "linux-armv7"
	case "mips":
		return "linux-mips_softfloat"
	case "mipsle":
		return "linux-mipsle_softfloat"
	default:
		return "linux-" + runtime.GOARCH
	}
}

func NormalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return DefaultRepo
	}
	return repo
}

func FetchLatest(repo string) (*ReleaseInfo, error) {
	repo = NormalizeRepo(repo)
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "keenetic-mtproto-updater")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("github %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var info ReleaseInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func Check(current, repo string) (*CheckResult, error) {
	repo = NormalizeRepo(repo)
	arch := ArchSuffix()
	info, err := FetchLatest(repo)
	if err != nil {
		return nil, err
	}
	want := "keenetic-mtproto-" + arch + ".tar.gz"
	assetURL := ""
	for _, a := range info.Assets {
		if a.Name == want {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	cur := strings.TrimSpace(current)
	if cur == "" {
		cur = "dev"
	}
	latest := strings.TrimSpace(info.TagName)
	out := &CheckResult{
		Current:     cur,
		Latest:      latest,
		UpdateAvail: versionLess(cur, latest) && assetURL != "",
		Asset:       want,
		URL:         assetURL,
		Arch:        arch,
		Repo:        repo,
	}
	if assetURL == "" {
		out.UpdateAvail = false
	}
	return out, nil
}

func versionLess(current, latest string) bool {
	c := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(current)), "v")
	l := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(latest)), "v")
	if c == "" || c == "dev" || c == "unknown" {
		return true
	}
	if c == l {
		return false
	}
	// Best-effort semver-ish compare without extra deps.
	cs := strings.Split(c, ".")
	ls := strings.Split(l, ".")
	n := len(cs)
	if len(ls) > n {
		n = len(ls)
	}
	for i := 0; i < n; i++ {
		var a, b int
		if i < len(cs) {
			fmt.Sscanf(cs[i], "%d", &a)
		}
		if i < len(ls) {
			fmt.Sscanf(ls[i], "%d", &b)
		}
		if a < b {
			return true
		}
		if a > b {
			return false
		}
	}
	return false
}

// Apply downloads the release asset and replaces the running binary in-place.
// Caller should restart the service afterwards.
func Apply(currentVersion, repo, binaryPath string) (*CheckResult, error) {
	check, err := Check(currentVersion, repo)
	if err != nil {
		return nil, err
	}
	if check.URL == "" {
		return check, fmt.Errorf("no release asset for arch %s", check.Arch)
	}

	tmpDir, err := os.MkdirTemp("", "kmt-update-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tgz := filepath.Join(tmpDir, "pkg.tar.gz")
	if err := downloadFile(check.URL, tgz); err != nil {
		return nil, err
	}
	binPath, err := extractBinary(tgz, tmpDir)
	if err != nil {
		return nil, err
	}

	if binaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		binaryPath, err = filepath.EvalSymlinks(exe)
		if err != nil {
			binaryPath = exe
		}
	}

	destDir := filepath.Dir(binaryPath)
	tmpBin := filepath.Join(destDir, ".keenetic-mtproto.new")
	if err := copyFile(binPath, tmpBin, 0o755); err != nil {
		return nil, err
	}
	bak := binaryPath + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(binaryPath, bak); err != nil {
		_ = os.Remove(tmpBin)
		return nil, fmt.Errorf("backup old binary: %w", err)
	}
	if err := os.Rename(tmpBin, binaryPath); err != nil {
		_ = os.Rename(bak, binaryPath)
		return nil, fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Chmod(binaryPath, 0o755)
	return check, nil
}

func RestartService() error {
	script := "/opt/etc/init.d/S95keenetic-mtproto"
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("service script not found: %s", script)
	}
	cmd := exec.Command(script, "restart")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start() // async: current process will be killed by restart
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "keenetic-mtproto-updater")
	client := &http.Client{Timeout: 3 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s", res.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, res.Body)
	return err
}

func extractBinary(tgz, dir string) (string, error) {
	f, err := os.Open(tgz)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		base := filepath.Base(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || base != "keenetic-mtproto" {
			continue
		}
		out := filepath.Join(dir, "keenetic-mtproto")
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			return "", err
		}
		w.Close()
		return out, nil
	}
	return "", fmt.Errorf("keenetic-mtproto binary not found in archive")
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

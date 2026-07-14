package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const DefaultRepo = "ARTJ1/keenetic-mtproto-main"

const defaultBinaryPath = "/opt/sbin/keenetic-mtproto"

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

func httpClient(timeout time.Duration) *http.Client {
	// Force HTTP/1.1 — HTTP/2 + some Keenetic softfloat builds mis-handle GitHub CDN redirects.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
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
	res, err := httpClient(30 * time.Second).Do(req)
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
	stableURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, want)
	browserURL := ""
	for _, a := range info.Assets {
		if a.Name == want {
			browserURL = a.BrowserDownloadURL
			break
		}
	}
	// Prefer /releases/latest/download/ — same URL install.sh uses successfully on Keenetic.
	assetURL := stableURL
	if browserURL == "" {
		assetURL = ""
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

func resolveBinaryPath(binaryPath string) (string, error) {
	if binaryPath != "" {
		return binaryPath, nil
	}
	if _, err := os.Stat(defaultBinaryPath); err == nil {
		return defaultBinaryPath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
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
	urls := downloadCandidates(check)
	var dlErr error
	for _, u := range urls {
		if err := downloadFile(u, tgz); err != nil {
			dlErr = err
			continue
		}
		dlErr = nil
		break
	}
	if dlErr != nil {
		return check, fmt.Errorf("download failed: %w", dlErr)
	}

	binPath, err := extractBinary(tgz, tmpDir)
	if err != nil {
		return check, err
	}

	binaryPath, err = resolveBinaryPath(binaryPath)
	if err != nil {
		return check, err
	}

	if err := installBinary(binPath, binaryPath); err != nil {
		return check, err
	}
	return check, nil
}

func downloadCandidates(check *CheckResult) []string {
	repo := NormalizeRepo(check.Repo)
	stable := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, check.Asset)
	tagged := ""
	if check.Latest != "" {
		tagged = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, check.Latest, check.Asset)
	}
	out := make([]string, 0, 3)
	for _, u := range []string{stable, tagged, check.URL} {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		dup := false
		for _, x := range out {
			if x == u {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, u)
		}
	}
	return out
}

func installBinary(src, dest string) error {
	destDir := filepath.Dir(dest)
	tmpBin := filepath.Join(destDir, ".keenetic-mtproto.new")
	if err := copyFile(src, tmpBin, 0o755); err != nil {
		return fmt.Errorf("stage new binary: %w", err)
	}
	bak := dest + ".bak"
	_ = os.Remove(bak)

	// Prefer rename; on ETXTBSY fall back to overwrite-via-copy after unlink.
	if err := os.Rename(dest, bak); err != nil {
		_ = os.Remove(tmpBin)
		// Last resort: copy over running path is often ETXTBSY too, so try remove+rename.
		if err2 := os.Remove(dest); err2 != nil {
			return fmt.Errorf("backup old binary: %v (remove: %v)", err, err2)
		}
		if err := os.Rename(tmpBin, dest); err != nil {
			return fmt.Errorf("install new binary: %w", err)
		}
		_ = os.Chmod(dest, 0o755)
		return nil
	}
	if err := os.Rename(tmpBin, dest); err != nil {
		_ = os.Rename(bak, dest)
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Chmod(dest, 0o755)
	return nil
}

func RestartService() error {
	script := "/opt/etc/init.d/S95keenetic-mtproto"
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("service script not found: %s", script)
	}
	cmd := exec.Command(script, "restart")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

// StartBackgroundApply writes a shell helper and runs it detached so download +
// replace + restart survive even if this process is killed mid-update.
func StartBackgroundApply(check *CheckResult, binaryPath string) error {
	if check == nil || check.URL == "" {
		return fmt.Errorf("no download URL")
	}
	binaryPath, err := resolveBinaryPath(binaryPath)
	if err != nil {
		return err
	}
	urls := downloadCandidates(check)
	if len(urls) == 0 {
		return fmt.Errorf("no download URL")
	}

	tmpDir := "/opt/tmp"
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		tmpDir = os.TempDir()
	}
	scriptPath := filepath.Join(tmpDir, "kmt-selfupdate.sh")
	logPath := filepath.Join(tmpDir, "kmt-selfupdate.log")
	tgz := filepath.Join(tmpDir, "kmt-update.tar.gz")
	extractDir := filepath.Join(tmpDir, "kmt-update-bin")

	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("exec >>" + shellQuote(logPath) + " 2>&1\n")
	b.WriteString("echo \"==== $(date) keenetic-mtproto self-update\"\n")
	b.WriteString("sleep 1\n")
	b.WriteString("TGZ=" + shellQuote(tgz) + "\n")
	b.WriteString("DIR=" + shellQuote(extractDir) + "\n")
	b.WriteString("DEST=" + shellQuote(binaryPath) + "\n")
	b.WriteString("rm -rf \"$DIR\" \"$TGZ\"\nmkdir -p \"$DIR\"\n")
	b.WriteString("download() {\n")
	b.WriteString("  url=\"$1\"\n")
	b.WriteString("  if command -v curl >/dev/null 2>&1; then curl -fsSL -o \"$TGZ\" \"$url\"; return $?; fi\n")
	b.WriteString("  if command -v wget >/dev/null 2>&1; then wget -qO \"$TGZ\" \"$url\"; return $?; fi\n")
	b.WriteString("  echo 'no curl/wget'; return 1\n")
	b.WriteString("}\n")
	b.WriteString("ok=0\n")
	for _, u := range urls {
		b.WriteString("if [ \"$ok\" -eq 0 ] && download " + shellQuote(u) + "; then ok=1; fi\n")
	}
	b.WriteString("[ \"$ok\" -eq 1 ] || { echo 'download failed'; exit 1; }\n")
	b.WriteString("tar -xzf \"$TGZ\" -C \"$DIR\"\n")
	b.WriteString("NEW=\"$DIR/keenetic-mtproto\"\n")
	b.WriteString("[ -f \"$NEW\" ] || NEW=$(find \"$DIR\" -type f -name keenetic-mtproto | head -n 1)\n")
	b.WriteString("[ -n \"$NEW\" ] && [ -f \"$NEW\" ] || { echo 'binary missing in archive'; exit 1; }\n")
	b.WriteString("chmod 755 \"$NEW\"\n")
	b.WriteString("cp -f \"$NEW\" \"$DEST.new\"\n")
	b.WriteString("chmod 755 \"$DEST.new\"\n")
	b.WriteString("mv -f \"$DEST\" \"$DEST.bak\" 2>/dev/null || rm -f \"$DEST\"\n")
	b.WriteString("mv -f \"$DEST.new\" \"$DEST\"\n")
	b.WriteString("chmod 755 \"$DEST\"\n")
	b.WriteString("rm -rf \"$DIR\" \"$TGZ\"\n")
	b.WriteString("if [ -x /opt/etc/init.d/S95keenetic-mtproto ]; then\n")
	b.WriteString("  /opt/etc/init.d/S95keenetic-mtproto restart || /opt/etc/init.d/S95keenetic-mtproto start\n")
	b.WriteString("fi\n")
	b.WriteString("echo done\n")

	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o755); err != nil {
		return err
	}

	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Dir = tmpDir
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	return cmd.Start()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func downloadFile(url, dest string) error {
	// Prefer Entware curl/wget — they already work for install.sh on the same routers.
	shellErr := downloadWithShell(url, dest)
	if shellErr == nil {
		return nil
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "keenetic-mtproto-updater")
	req.Header.Set("Accept", "*/*")
	res, err := httpClient(3 * time.Minute).Do(req)
	if err != nil {
		if shellErr != nil && !os.IsNotExist(shellErr) {
			return fmt.Errorf("%v; go http: %w", shellErr, err)
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 180))
		msg := strings.TrimSpace(string(b))
		goErr := fmt.Errorf("download %s", res.Status)
		if msg != "" {
			goErr = fmt.Errorf("download %s: %s", res.Status, msg)
		}
		if shellErr != nil && !os.IsNotExist(shellErr) {
			return fmt.Errorf("%v; %w", shellErr, goErr)
		}
		return goErr
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, res.Body)
	if err != nil {
		return err
	}
	if n < 1024 {
		return fmt.Errorf("download too small (%d bytes)", n)
	}
	return nil
}

func downloadWithShell(url, dest string) error {
	if p, err := exec.LookPath("curl"); err == nil {
		cmd := exec.Command(p, "-fsSL", "-o", dest, url)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("curl: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if p, err := exec.LookPath("wget"); err == nil {
		cmd := exec.Command(p, "-qO", dest, url)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("wget: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return os.ErrNotExist
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

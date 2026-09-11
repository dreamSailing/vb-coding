package update

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ProgressFn 进度回调：done/total 为字节数，total<=0 表示未知。
type ProgressFn func(done, total int64)

// ApplyOutcome 描述一次自升级实际落盘的内容，供 CLI 输出与测试断言。
type ApplyOutcome struct {
	BinaryPath  string // 替换后的二进制路径
	CoreUpdated bool   // 是否同步替换了 exe 同级的 core/ 目录
}

// ApplyWithClient 下载 CheckResult 选中的平台归档，校验 SHA256，解压并原地替换：
//   - 二进制：Windows 下先把旧文件改名 .bak 再换新（运行中的 exe 不可删除、
//     可改名），其余平台直接原子 rename；
//   - core/：Release 归档自带 sidecar（eos + core/<triple>/）。无论当前是
//     Release 布局（exe 旁有 core/）还是 go install 布局（无 core/），都把
//     新 core/ 落到 exe 同级——resolver 优先读 exe 同级，内核随之升级。
// 允许注入显式代理客户端（nil = 默认）。
func ApplyWithClient(ctx context.Context, res *CheckResult, progress ProgressFn, client *http.Client) (*ApplyOutcome, error) {
	if res == nil || res.DownloadURL == "" || res.AssetName == "" {
		return nil, errors.New("no downloadable asset for this platform")
	}

	exePath, err := currentExecutable()
	if err != nil {
		return nil, err
	}
	installDir := filepath.Dir(exePath)

	tmpDir, err := os.MkdirTemp("", "eos-update-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 下载归档与校验清单
	archivePath := filepath.Join(tmpDir, res.AssetName)
	if err := downloadTo(ctx, res.DownloadURL, archivePath, progress, client); err != nil {
		return nil, fmt.Errorf("download %s: %w", res.AssetName, err)
	}

	// 2. SHA256 校验（清单缺失时拒绝安装——自升级不接受未校验的产物）
	if res.ChecksumURL == "" {
		return nil, errors.New("release has no SHA256SUMS.txt asset")
	}
	sumsPath := filepath.Join(tmpDir, "SHA256SUMS.txt")
	if err := downloadTo(ctx, res.ChecksumURL, sumsPath, nil, client); err != nil {
		return nil, fmt.Errorf("download SHA256SUMS.txt: %w", err)
	}
	if err := verifyChecksum(archivePath, sumsPath, res.AssetName); err != nil {
		return nil, err
	}

	// 3. 解压到独立目录
	extractDir := filepath.Join(tmpDir, "extract")
	if err := extractArchive(archivePath, extractDir); err != nil {
		return nil, fmt.Errorf("extract %s: %w", res.AssetName, err)
	}
	stageRoot, err := singleRootDir(extractDir)
	if err != nil {
		return nil, err
	}

	// 4. 定位新二进制（eos / eos.exe）
	binaryName := "eos"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	newBinary := filepath.Join(stageRoot, binaryName)
	if _, err := os.Stat(newBinary); err != nil {
		return nil, fmt.Errorf("binary %s not found in archive: %w", binaryName, err)
	}

	// 5. 替换二进制（Windows 运行中自替换需先改名让位）
	if err := replaceBinary(newBinary, exePath); err != nil {
		return nil, err
	}

	// 6. 同步 core/（存在则整目录换新；Windows 下旧 core 里的 eos-core.exe
	// 可能仍在运行，改名目录通常可行、删除失败则留待下次清理）
	outcome := &ApplyOutcome{BinaryPath: exePath}
	stageCore := filepath.Join(stageRoot, "core")
	if info, err := os.Stat(stageCore); err == nil && info.IsDir() {
		if err := swapCoreDir(stageCore, filepath.Join(installDir, "core")); err != nil {
			return nil, fmt.Errorf("swap core dir: %w", err)
		}
		outcome.CoreUpdated = true
	}

	cleanupStaleCores(installDir)
	return outcome, nil
}

func currentExecutable() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}
	return exePath, nil
}

// 弱网（如直连 GitHub 的国内链路）单次 GET 极易瞬断：主包侥幸下完、
// 几 KB 的 SHA256SUMS.txt 被 RST 的情况实测常见。下载统一带重试 + 断点
// 续传（Range），重试从已落盘字节继续，不做整包重来。
var (
	downloadAttempts      = 4
	downloadRetryBackoff  = 1 * time.Second
)

func downloadTo(ctx context.Context, url, dst string, progress ProgressFn, client *http.Client) error {
	var lastErr error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		// 重试时若已有部分落盘且服务端支持 Range，从断点继续；
		// 首次尝试若有残留（上次调用失败遗留）则丢弃重新开始。
		offset := int64(0)
		if attempt > 1 {
			if info, err := os.Stat(dst); err == nil {
				offset = info.Size()
			}
		} else {
			_ = os.Remove(dst)
		}
		written, err := downloadOnce(ctx, url, dst, offset, progress, client)
		if err == nil {
			return nil
		}
		lastErr = err
		if written == 0 {
			// 本次尝试毫无进展（连接都没建立或立即断开），残留文件清掉，
			// 避免下次 Range 从 0 字节"续传"造成误导。
			_ = os.Remove(dst)
		}
		if attempt == downloadAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * downloadRetryBackoff):
		}
	}
	return fmt.Errorf("after %d attempts: %w", downloadAttempts, lastErr)
}

// downloadOnce 执行一次下载。offset>0 时带 Range 请求；服务端返回 206
// 则续传追加，返回 200（不支持 Range）则整体重写。返回本次写入字节数。
func downloadOnce(ctx context.Context, url, dst string, offset int64, progress ProgressFn, client *http.Client) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "EOS-Update")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	// client 为 nil 用默认客户端（遵循环境代理约定），与历史行为一致。
	activeClient := http.DefaultClient
	if client != nil {
		activeClient = client
	}
	resp, err := activeClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var f *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		offset = 0
		if f, err = os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644); err != nil {
			return 0, err
		}
	case http.StatusPartialContent:
		if f, err = os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	defer f.Close()

	total := resp.ContentLength
	if total >= 0 {
		total += offset
	}
	var reader io.Reader = resp.Body
	if progress != nil {
		reader = &progressReader{r: resp.Body, base: offset, total: total, fn: progress}
	}
	written, err := io.Copy(f, reader)
	return written, err
}

type progressReader struct {
	r     io.Reader
	base  int64 // 续传时已落盘的字节数，进度按累计值上报
	total int64
	done  int64
	fn    ProgressFn
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.fn(p.base+p.done, p.total)
	return n, err
}

func verifyChecksum(archivePath, sumsPath, assetName string) error {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return fmt.Errorf("read SHA256SUMS.txt: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s not listed in SHA256SUMS.txt", assetName)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("checksum mismatch: want %s got %s", want, got)
	}
	return nil
}

func extractArchive(archivePath, dstDir string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, dstDir)
	}
	return extractTarGz(archivePath, dstDir)
}

func extractZip(archivePath, dstDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		path, err := safeJoin(dstDir, f.Name)
		if err != nil {
			continue
		}
		// 目录条目判定不能只看 zip.FileInfo().IsDir()：它只认条目名以 "/"
		// 结尾。Windows PowerShell Compress-Archive 的历史产物以反斜杠写
		// 目录条目（...\core\），不识别会当文件落盘导致解压失败。
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, "\\") {
			os.MkdirAll(path, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, dstDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(dstDir, header.Name)
		if err != nil {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode)&0o777|0o400)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		}
	}
}

// safeJoin 拒绝归档内的绝对路径与 .. 穿越，防止恶意归档逃逸出 dstDir。
// name 先把反斜杠归一化为正斜杠：ZIP 规范要求 "/" 作分隔，但 Windows 的
// PowerShell Compress-Archive 会以 "\" 写入条目（历史产物如此），解压器
// 只按 "/" 识别目录后缀（zip.FileInfo().IsDir()），不归一化会把目录当文件。
func safeJoin(dstDir, name string) (string, error) {
	name = filepath.ToSlash(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return filepath.Join(dstDir, filepath.FromSlash(name)), nil
}

// singleRootDir 归档通常形如 eos-cli_<ver>_<target>/...，返回唯一顶层目录；
// 顶层即文件时（无包裹目录）返回解压根目录。
func singleRootDir(extractDir string) (string, error) {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(extractDir, entries[0].Name()), nil
	}
	return extractDir, nil
}

func replaceBinary(newBinary, exePath string) error {
	incoming := exePath + ".eos-new"
	if err := copyExecutable(newBinary, incoming); err != nil {
		return fmt.Errorf("stage new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		// 运行中的 exe 不能删除但可以改名：旧 → .bak，新 → 目标名。
		backup := exePath + ".bak"
		_ = os.Remove(backup)
		if err := os.Rename(exePath, backup); err != nil {
			os.Remove(incoming)
			return fmt.Errorf("backup current binary: %w", err)
		}
		if err := os.Rename(incoming, exePath); err != nil {
			os.Rename(backup, exePath)
			return fmt.Errorf("move new binary in place: %w", err)
		}
		_ = os.Remove(backup) // 运行中删除可能失败，残留由 cleanupStaleCores 兜底不了，留 .bak 无害
		return nil
	}

	if err := os.Rename(incoming, exePath); err != nil {
		os.Remove(incoming)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func swapCoreDir(stageCore, targetCore string) error {
	if err := os.MkdirAll(filepath.Dir(targetCore), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(targetCore); err == nil {
		stale := fmt.Sprintf("%s.old-%d", targetCore, time.Now().Unix())
		if err := os.Rename(targetCore, stale); err != nil {
			// Windows 下旧 core/eos-core.exe 仍在运行时改名目录通常可行；
			// 真失败则放弃换新（内核沿用旧版，二进制已更新，resolver 兼容旧 sidecar）。
			return fmt.Errorf("rotate old core dir: %w", err)
		}
	}
	// 跨设备移动：copy 兜底
	if err := os.Rename(stageCore, targetCore); err != nil {
		return copyDir(stageCore, targetCore)
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// cleanupStaleCores 清理历史升级残留的 core.old-* 目录（Windows 上运行中的
// 旧 sidecar 无法即时删除）。仅匹配该前缀，避免误删用户目录。
func cleanupStaleCores(installDir string) {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "core.old-") {
			_ = os.RemoveAll(filepath.Join(installDir, e.Name()))
		}
	}
}

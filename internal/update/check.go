package update

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/version"
)

const githubOwner = "eosaios"
const githubRepo = "eos"
// checkTimeout 是单次尝试的超时预算。var 以便测试注入短超时。
var checkTimeout = 15 * time.Second

type CheckResult struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	// AssetName / DownloadURL 指向当前平台对应的发布归档
	// （eos-cli_<版本>_<goos>-<goarch>.tar.gz / .zip），而非裸二进制。
	AssetName string `json:"assetName,omitempty"`
	// ChecksumURL 指向该 Release 的 SHA256SUMS.txt，Apply 阶段校验归档完整性。
	ChecksumURL string `json:"checksumUrl,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	ReleaseURL  string `json:"releaseUrl,omitempty"`
}

// CheckLatestWithClient 用指定客户端检查更新（nil = 默认，遵循环境代理）。
func CheckLatestWithClient(ctx context.Context, client *http.Client) (*CheckResult, error) {
	return checkLatestFor(ctx, runtime.GOOS, runtime.GOARCH, client)
}

func checkLatestFor(ctx context.Context, goos, goarch string, client *http.Client) (*CheckResult, error) {
	current := strings.TrimSpace(version.AppVersion)
	if current == "" || current == "dev" {
		return &CheckResult{CurrentVersion: current, HasUpdate: false}, nil
	}

	latest, err := fetchLatestTag(ctx, latestReleasesPageURL(), client)
	if err != nil {
		return nil, err
	}

	return buildCheckResult(current, latest, goos, goarch), nil
}

// latestReleasesPageURL 返回 releases/latest 网页地址（非 API 端点）。
func latestReleasesPageURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest", githubOwner, githubRepo)
}

// fetchLatestTag 通过 releases/latest 网页重定向解析最新版本号。
// 不走 api.github.com：未认证限流 60 次/小时/IP，共享代理出口（国内常态）
// 极易触发 403；网页会 302 到 releases/tag/<版本>，从 Location 解析 tag
// 不占 API 配额。与 scripts/install.ps1 同一方案。
// 弱网下瞬断常见（EOF/RST/整段黑洞），带退避重试；每次尝试持有独立的
// checkTimeout 预算——若共享一个总预算，首次超时（最长 15s）就几乎耗尽
// 窗口，后续重试拿不到完整预算形同虚设（beta.14 实测如此）。
func fetchLatestTag(ctx context.Context, page string, client *http.Client) (string, error) {
	// client 非 nil 时取其 Transport（显式代理），否则用默认 Transport
	//（遵循环境代理约定）；无论哪种源，检查路径统一带重定向陷阱——不跟随
	// 302、直接取 Location 解析 tag（见下方注释）。
	transport := http.DefaultTransport
	if client != nil && client.Transport != nil {
		transport = client.Transport
	}
	trapClient := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 不跟随重定向，直接取 302 的 Location
		},
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, page, nil)
		if err != nil {
			cancel()
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "EOS-Update-Check")

		resp, err := trapClient.Do(req)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("resolve latest release: %w", err)
		} else {
			tag, err := tagFromRedirect(resp)
			resp.Body.Close()
			if err == nil {
				return tag, nil
			}
			// 非 3xx/缺 Location 是终态错误（如限流页），短时间窗内结果
			// 不变，重试无意义，直接失败。
			return "", err
		}
		if attempt == 3 {
			break
		}
		select {
		case <-ctx.Done():
			return "", lastErr
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return "", lastErr
}

// tagFromRedirect 从 releases/latest 的重定向响应中解析版本号。
// 非 3xx/缺 Location 视为终态错误（如限流页），不重试——短时间窗内结果不变。
func tagFromRedirect(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	loc := strings.TrimSpace(resp.Header.Get("Location"))
	const marker = "/tag/"
	if loc == "" || !strings.Contains(loc, marker) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("releases/latest 未返回版本重定向（HTTP %d）: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	tag := strings.TrimSuffix(loc[strings.LastIndex(loc, marker)+len(marker):], "/")
	if tag == "" {
		return "", fmt.Errorf("releases/latest 重定向缺少版本号: %s", loc)
	}
	return tag, nil
}

// buildCheckResult 由最新版本号按发布资产命名约定构造检查结果。
// 归档与校验文件 URL 均为确定性拼接（releases/download/<tag>/<资产名>，
// 命名与 CI 产物一致），无需再请求 API 枚举 assets。
func buildCheckResult(current, latest, goos, goarch string) *CheckResult {
	result := &CheckResult{
		CurrentVersion: current,
		LatestVersion:  latest,
		HasUpdate:      isNewer(current, latest),
		ReleaseURL:     fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", githubOwner, githubRepo, latest),
	}

	assetName, _ := platformAssetName(latest, goos, goarch)
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", githubOwner, githubRepo, latest)
	result.AssetName = assetName
	result.DownloadURL = base + "/" + assetName
	result.ChecksumURL = base + "/SHA256SUMS.txt"
	return result
}

// platformAssetName 返回 goos/goarch 对应的发布归档名（与 .github 发布
// 资产命名约定一致）：非 Windows 为 .tar.gz，Windows 为 .zip。
func platformAssetName(tag, goos, goarch string) (string, string) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	suffix := ".tar.gz"
	if goos == "windows" {
		suffix = ".zip"
	}
	return fmt.Sprintf("eos-cli_v%s_%s-%s%s", tag, goos, goarch, suffix), suffix
}

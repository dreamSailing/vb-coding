package render

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/muesli/termenv"
)

// DefaultChromaTheme 是 diff/代码块高亮的默认 chroma 主题。
const DefaultChromaTheme = "monokai"

// NormalizeChromaTheme 校验主题名：空白或 chroma 不认识的名字回默认主题。
// 注意 styles.Get 对未知名返回 Fallback（非 nil），必须比对 Style.Name 判定。
func NormalizeChromaTheme(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return DefaultChromaTheme
	}
	for _, candidate := range []string{trimmed, strings.ToLower(trimmed)} {
		if style := styles.Get(candidate); style != nil && style.Name == candidate {
			return candidate
		}
	}
	return DefaultChromaTheme
}

func chromaFormatter() chroma.Formatter {
	formatterName := "terminal16"
	switch termenv.EnvColorProfile() {
	case termenv.ANSI256:
		formatterName = "terminal256"
	case termenv.TrueColor:
		formatterName = "terminal16m"
	}
	return formatters.Get(formatterName)
}

func highlightANSI(code string, lang string, theme string) string {
	code = strings.ReplaceAll(code, "\r\n", "\n")
	code = strings.TrimRight(code, "\n")
	if code == "" {
		return ""
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "js":
		lang = "javascript"
	case "ts":
		lang = "typescript"
	case "py":
		lang = "python"
	case "sh":
		lang = "bash"
	case "yml":
		lang = "yaml"
	}

	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	style := styles.Get(NormalizeChromaTheme(theme))
	if style == nil {
		style = styles.Fallback
	}

	var buf bytes.Buffer
	if err := chromaFormatter().Format(&buf, style, it); err != nil {
		return code
	}
	return strings.TrimRight(buf.String(), "\n")
}

// HighlightDiffANSI 用 chroma 的 diff lexer 渲染统一 diff 的 ANSI 高亮
//（+/-/@@ 标记行着色）。theme 会被 NormalizeChromaTheme 校验回退。
// 注意：输入必须是原始 diff 文本；截断应在调用前完成，避免截断 ANSI 序列。
func HighlightDiffANSI(diff string, theme string) string {
	return highlightANSI(diff, "diff", theme)
}

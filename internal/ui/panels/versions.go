package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// VersionsPanel 版本历史面板
type VersionsPanel struct {
	BasePanel
	styles   *styles.Styles
	language string
	files    list.Model
	versions list.Model
	mode     string // "files" or "versions"
}

// FileItem 文件列表项
type FileItem struct {
	Path  string
	Count int
	Last  time.Time
}

func (f FileItem) FilterValue() string { return f.Path }
func (f FileItem) Title() string {
	if f.Count > 0 {
		return fmt.Sprintf("%s (%d)", f.Path, f.Count)
	}
	return f.Path
}
func (f FileItem) Description() string {
	if f.Last.IsZero() {
		return ""
	}
	return f.Last.Format("2006-01-02 15:04:05")
}

// VersionItem 版本列表项
type VersionItem struct {
	Timestamp string
	FilePath  string
	Size      int
}

func (v VersionItem) FilterValue() string { return v.Timestamp }
func (v VersionItem) Title() string       { return v.Timestamp }
func (v VersionItem) Description() string {
	if v.Size > 0 {
		return fmt.Sprintf("%d bytes", v.Size)
	}
	return v.FilePath
}

// NewVersionsPanel 创建新的版本历史面板
func NewVersionsPanel(styles *styles.Styles) *VersionsPanel {
	fileList := list.New([]list.Item{}, list.NewDefaultDelegate(), 60, 20)
	fileList.Title = ""
	fileList.SetShowTitle(false)
	fileList.SetShowStatusBar(false)
	fileList.SetShowPagination(false)
	fileList.SetShowHelp(false)
	fileList.SetFilteringEnabled(false)

	versionList := list.New([]list.Item{}, list.NewDefaultDelegate(), 60, 20)
	versionList.Title = ""
	versionList.SetShowTitle(false)
	versionList.SetShowStatusBar(false)
	versionList.SetShowPagination(false)
	versionList.SetShowHelp(false)
	versionList.SetFilteringEnabled(false)

	return &VersionsPanel{
		BasePanel: NewBasePanel("versions"),
		styles:    styles,
		language:  "zh",
		files:     fileList,
		versions:  versionList,
		mode:      "files",
	}
}

func (p *VersionsPanel) SetLanguage(lang string) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = "zh"
	}
	p.language = lang
}

func (p *VersionsPanel) Reset() {
	p.mode = "files"
	p.versions.SetItems(nil)
}

// SetFiles 设置文件列表
func (p *VersionsPanel) SetFiles(files []FileItem) {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = f
	}
	p.files.SetItems(items)
}

// SetVersions 设置版本列表
func (p *VersionsPanel) SetVersions(filePath string, versions []VersionItem) {
	items := make([]list.Item, len(versions))
	for i, v := range versions {
		v.FilePath = filePath
		items[i] = v
	}
	p.versions.SetItems(items)
	p.mode = "versions"
}

// Init 初始化
func (p *VersionsPanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *VersionsPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	var cmd tea.Cmd

	switch p.mode {
	case "files":
		p.files, cmd = p.files.Update(msg)
	case "versions":
		p.versions, cmd = p.versions.Update(msg)
	}

	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.SetLanguage(msg.Language)
		return p, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if p.mode == "files" {
				if item, ok := p.files.SelectedItem().(FileItem); ok {
					return p, func() tea.Msg {
						return VersionsLoadMsg{FilePath: item.Path}
					}
				}
			}
		case "r":
			// 回滚
			if p.mode == "versions" {
				if item, ok := p.versions.SelectedItem().(VersionItem); ok {
					return p, func() tea.Msg {
						return VersionsRollbackMsg{FilePath: item.FilePath, Timestamp: item.Timestamp}
					}
				}
			}
		case "x":
			// 删除版本
			if p.mode == "versions" {
				if item, ok := p.versions.SelectedItem().(VersionItem); ok {
					return p, func() tea.Msg {
						return VersionsDeleteMsg{FilePath: item.FilePath, Timestamp: item.Timestamp}
					}
				}
			} else {
				// 删除文件的所有版本
				if item, ok := p.files.SelectedItem().(FileItem); ok {
					return p, func() tea.Msg {
						return VersionsDeleteFileMsg{FilePath: item.Path}
					}
				}
			}
		case "ctrl+x":
			// 删除所有版本
			return p, func() tea.Msg {
				return VersionsDeleteAllMsg{}
			}
		case "esc":
			if p.mode == "versions" {
				p.mode = "files"
				return p, nil
			}
		}
	}

	return p, cmd
}

// View 渲染
func (p *VersionsPanel) View() string {
	var content strings.Builder

	switch p.mode {
	case "files":
		content.WriteString(p.styles.PanelTitle.Render(i18n.T("versions.title.files", p.language)))
		content.WriteString("\n\n")
		content.WriteString(p.files.View())
		content.WriteString("\n\n")
		content.WriteString(p.styles.TextMuted.Render(
			i18n.T("versions.help.files", p.language)))
	case "versions":
		content.WriteString(p.styles.PanelTitle.Render(i18n.T("versions.title.versions", p.language)))
		content.WriteString("\n\n")
		content.WriteString(p.versions.View())
		content.WriteString("\n\n")
		content.WriteString(p.styles.TextMuted.Render(
			i18n.T("versions.help.versions", p.language)))
	}

	return p.RenderBorder(content.String(), i18n.T("versions.panel", p.language))
}

// SetSize 设置大小
func (p *VersionsPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.files.SetSize(width-4, height-8)
	p.versions.SetSize(width-4, height-8)
}

// VersionsLoadMsg 加载版本消息
type VersionsLoadMsg struct {
	FilePath string
}

// VersionsRollbackMsg 回滚版本消息
type VersionsRollbackMsg struct {
	FilePath  string
	Timestamp string
}

// VersionsDeleteMsg 删除版本消息
type VersionsDeleteMsg struct {
	FilePath  string
	Timestamp string
}

// VersionsDeleteFileMsg 删除文件所有版本消息
type VersionsDeleteFileMsg struct {
	FilePath string
}

// VersionsDeleteAllMsg 删除所有版本消息
type VersionsDeleteAllMsg struct{}

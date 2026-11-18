package main

import (
	"strings"

	"easydict-linux/internal/services"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// view builds the window, from the state, for every frame.
func (a *app) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.header(c)
		if len(a.ids) == 0 {
			a.welcome(c, t)
		} else {
			a.tabBar(c, t)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Padding(12, 20, 20, 20).Gap(12).Children(func() {
					for _, id := range a.visibleIDs() {
						a.card(c, t, a.results[id])
					}
				})
			})
		}
		a.settingsModal(c, t)
	})
}

func (a *app) header(c *ui.Context) {
	ui.Column(c).Padding(16, 20, 12, 20).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Select(c, &a.from, labelsOf(fromLangs)).Width(128).Label("源语言").Changed() && a.query != "" {
				a.search(a.query)
			}
			if ui.Button(c, "⇄").Label("交换语言").Clicked() {
				a.swap()
			}
			if ui.Select(c, &a.to, labelsOf(toLangs)).Width(128).Label("目标语言").Changed() {
				a.manualTarget = true
				if a.query != "" {
					a.search(a.query)
				}
			}
			ui.Spacer(c)
			if ui.Button(c, "设置").Clicked() {
				a.openSettings()
			}
		})

		ui.TextArea(c, &a.query).FillWidth().Height(92).AutoFocus().
			Label("原文").Placeholder("输入单词或粘贴一段文字").
			OnShortcut(ui.Cmd, ui.KeyEnter, func() { a.search(a.query) })

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "粘贴并翻译").Clicked() {
				a.paste()
			}
			if a.query != "" && ui.Button(c, "清空").Clicked() {
				a.query = ""
				a.search("")
			}
			ui.Spacer(c)
			if a.busy() {
				if ui.Button(c, "停止").Clicked() {
					a.cancel()
				}
			}
			if ui.PrimaryButton(c, "翻译").Tooltip("⌘↩").Disabled(strings.TrimSpace(a.query) == "").Clicked() {
				a.search(a.query)
			}
		})
		ui.Divider(c)
	})
}

func (a *app) tabBar(c *ui.Context, t *ui.Theme) {
	labels := []string{"全部"}
	for _, id := range a.ids {
		label := shortNames[id]
		if label == "" {
			label = id
		}
		labels = append(labels, label)
	}
	ui.Column(c).Gap(6).Padding(0, 20).Children(func() {
		if string(a.request.ToLang) != a.toCode() {
			target := a.to
			setLang(toLangs, string(a.request.ToLang), &target)
			ui.Text(c, "本次自动换向至"+target).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Tabs(c, &a.tab, labels...).Label("翻译服务")
	})
}

func (a *app) visibleIDs() []string {
	if a.tab <= 0 || a.tab > len(a.ids) {
		return a.ids
	}
	return a.ids[a.tab-1 : a.tab]
}

func (a *app) welcome(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Center().Gap(10).Children(func() {
		if len(a.activeServices()) == 0 {
			ui.Text(c, "请在设置中启用并配置翻译服务").TextColor(t.TextMuted)
		} else {
			ui.Text(c, "输入原文开始翻译").FontSize(17).Bold()
			ui.Text(c, "⌘↩ 翻译  ·  ↩ 换行").FontSize(13).TextColor(t.TextMuted)
		}
	})
}

// card shows one service's answer.
func (a *app) card(c *ui.Context, t *ui.Theme, r *services.ServiceResult) {
	if r == nil {
		return
	}
	ui.Column(c).
		FillWidth().
		Background(t.Surface).
		Radius(10).
		Border(1, t.Border).
		Padding(14).
		Gap(10).
		Children(func() {
			a.cardHeader(c, t, r)
			if r.Status != "success" {
				return
			}
			a.cardBody(c, t, r)
		})
}

func (a *app) cardHeader(c *ui.Context, t *ui.Theme, r *services.ServiceResult) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, r.ServiceName).FontSize(13).Bold()
		if r.Status == "success" && r.IsDictionary {
			ui.Badge(c, "词典")
		}
		ui.Spacer(c)
		switch r.Status {
		case "loading":
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label("查询中")
				ui.Text(c, "查询中…").FontSize(11).TextColor(t.TextMuted)
			})
		case "cancelled":
			ui.Text(c, "已取消").FontSize(11).TextColor(t.TextMuted)
		case "error":
			ui.Text(c, "失败").FontSize(11).TextColor(t.Danger)
		}
		if r.Status != "loading" && r.Status != "cancelled" {
			if r.Status == "success" {
				if ui.Button(c, "朗读").FontSize(11).Padding(3, 8).Tooltip("朗读结果").Clicked() {
					a.speakText(plainText(r))
				}
			}
			if ui.Button(c, "复制").FontSize(11).Padding(3, 8).Clicked() {
				if text := plainText(r); text != "" {
					mygo.Clipboard.WriteText(text)
					c.Toast("已复制")
				}
			}
			if ui.Button(c, "重试").FontSize(11).Padding(3, 8).Clicked() {
				a.refresh(r.ServiceID)
			}
		}
	})

	if r.Status == "error" && r.Error != "" {
		ui.Text(c, r.Error).FontSize(12).TextColor(t.Danger)
	}
}

func (a *app) cardBody(c *ui.Context, t *ui.Theme, r *services.ServiceResult) {
	if len(r.Phonetics) > 0 {
		ui.Row(c).Gap(8).Children(func() {
			for _, p := range r.Phonetics {
				p := p
				ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(3, 8).Radius(6).Background(t.Background).Children(func() {
					if p.Type != "" {
						ui.Text(c, strings.ToUpper(p.Type)).FontSize(10).TextColor(t.TextMuted)
					}
					ui.Text(c, p.Symbol).FontSize(13)
					if ui.Button(c, "▶").FontSize(9).Padding(2, 6).Tooltip("朗读").Clicked() {
						if p.AudioURL != "" {
							a.speak(p.AudioURL)
						} else {
							a.speakText(a.request.Text)
						}
					}
				})
			}
		})
	}

	if r.IsDictionary && len(r.Definitions) > 0 {
		for _, d := range r.Definitions {
			ui.RichText(c,
				ui.Span{Text: d.Pos + "  ", Size: 11, Weight: 500, Color: t.Accent},
				ui.Span{Text: strings.Join(d.Meanings, "；"), Size: 14},
			).FillWidth()
		}
	} else if r.Translation != "" {
		if r.ServiceID == "ai" {
			markdown.View(c, r.Translation, &a.md)
		} else {
			ui.Text(c, r.Translation).FontSize(15).FillWidth()
		}
	}

	if len(r.Sentences) > 0 {
		ui.Divider(c)
		for i, s := range r.Sentences {
			if i >= 3 {
				break
			}
			ui.Column(c).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s.Source).FontSize(13).Grow(1)
					if s.SourceDict != "" {
						ui.Text(c, s.SourceDict).FontSize(10).TextColor(t.TextMuted)
					}
				})
				ui.Text(c, s.Target).FontSize(13).TextColor(t.TextMuted)
			})
		}
	}
}

// settingsModal edits a copy of the settings; 保存 writes it to the config.
func (a *app) settingsModal(c *ui.Context, t *ui.Theme) {
	ui.Modal(c, &a.st.open, func() {
		ui.Column(c).Width(480).Gap(12).Padding(18).Children(func() {
			ui.Text(c, "设置").FontSize(18).Bold()
			ui.Scroll(c).MaxHeight(460).Children(func() {
				ui.Form(c, func() {
					ui.Fieldset(c, "翻译服务", func() {
						ui.Checkbox(c, &a.st.youdao, "有道词典")
						ui.Checkbox(c, &a.st.bing, "Bing")
						ui.Checkbox(c, &a.st.google, "Google")
						ui.Checkbox(c, &a.st.libre, "LibreTranslate（自托管）")
						ui.Checkbox(c, &a.st.ai, "AI 释义")
						ui.Checkbox(c, &a.st.deepl, "DeepL")
					})
					ui.Fieldset(c, "语言与界面", func() {
						ui.Field(c, "默认目标语言", func() {
							ui.Select(c, &a.st.target, labelsOf(toLangs)).Width(190)
						})
						ui.Field(c, "主题", func() {
							ui.Select(c, &a.st.theme, labelsOf(themeLangs)).Width(150)
						})
						ui.Field(c, "窗口置顶", func() { ui.Switch(c, &a.st.top) })
					})
					ui.Fieldset(c, "LibreTranslate", func() {
						ui.Field(c, "服务地址", func() {
							ui.TextInput(c, &a.st.libreURL).Width(250).Placeholder("http://localhost:5000")
						})
					})
					ui.Fieldset(c, "AI 释义", func() {
						ui.Field(c, "接口地址", func() {
							ui.TextInput(c, &a.st.aiBase).Width(250).Placeholder("https://api.deepseek.com/v1")
						})
						ui.Field(c, "API Key", func() { ui.TextInput(c, &a.st.aiKey).Password().Width(250) })
						ui.Field(c, "模型", func() { ui.TextInput(c, &a.st.aiModel).Width(250) })
					})
					ui.Fieldset(c, "DeepL", func() {
						ui.Field(c, "密钥", func() { ui.TextInput(c, &a.st.deeplKey).Password().Width(250) })
						ui.Field(c, "免费版", func() { ui.Switch(c, &a.st.deeplFree) })
					})
				})
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.st.open = false
				}
				if ui.PrimaryButton(c, "保存").Clicked() {
					if err := a.saveSettings(); err != nil {
						c.Toast("保存失败：" + err.Error())
					} else {
						c.Toast("设置已保存")
					}
				}
			})
		})
	})
}

func plainText(r *services.ServiceResult) string {
	if r.Translation != "" {
		return r.Translation
	}
	var parts []string
	for _, d := range r.Definitions {
		line := strings.Join(d.Meanings, "；")
		if d.Pos != "" {
			line = d.Pos + " " + line
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

// Easydict for macOS, drawn by MyGo itself: the same query engine as the
// Linux app (internal/services and internal/config), shown by package ui in
// a window without a web page.
//
//	go run ./cmd/easydict-native
package main

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"easydict-linux/internal/config"
	"easydict-linux/internal/services"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/mp3"
	"github.com/egoist/mygo/plugins/updater"
	updaterui "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type lang struct{ code, label string }

//go:embed icon.png
var iconPNG []byte

var fromLangs = []lang{
	{"auto", "自动识别"},
	{"en", "英语"},
	{"zh-CN", "简体中文"},
	{"zh-TW", "繁体中文"},
	{"ja", "日语"},
	{"ko", "韩语"},
	{"fr", "法语"},
	{"de", "德语"},
}

var toLangs = []lang{
	{"zh-CN", "简体中文"},
	{"zh-TW", "繁体中文"},
	{"en", "英语"},
	{"ja", "日语"},
	{"ko", "韩语"},
	{"fr", "法语"},
	{"de", "德语"},
}

var themeLangs = []lang{
	{"auto", "跟随系统"},
	{"light", "浅色"},
	{"dark", "深色"},
}

// short names for the tab bar, which the providers' own are too long for.
var shortNames = map[string]string{
	"youdao":         "有道词典",
	"bing":           "Bing",
	"google":         "Google",
	"ai":             "AI 释义",
	"deepl":          "DeepL",
	"libretranslate": "LibreTranslate",
}

type app struct {
	win     *mygo.Window
	manager *services.Manager
	cfg     *config.Config

	query        string
	from         string
	to           string
	manualTarget bool

	// tab is 0 for every service, 1 + i for the i-th of ids.
	tab     int
	ids     []string
	results map[string]*services.ServiceResult
	request services.QueryRequest
	// gen identifies the query results belong to, so that a slow answer to
	// an old query changes nothing.
	gen int

	md markdown.Options

	// player holds the sound being played, so that it is not collected
	// while it plays and that the next one stops it.
	player *audio.Player
	// say is the system voice started for a result without an audio URL.
	say *exec.Cmd

	capturePath string
	captured    bool

	// st is the settings dialog and the copy of the settings it edits.
	st settingsState
}

func main() {
	query := flag.String("query", "", "query to run on launch")
	capture := flag.String("capture", "", "write a PNG of the window to this path and quit")
	from := flag.String("from", "", "source language code, as en")
	to := flag.String("to", "", "target language code, as zh-CN")
	flag.Parse()

	if *query == "" && flag.NArg() > 0 {
		*query = strings.Join(flag.Args(), " ")
	}

	mygo.Use(updaterui.New(updater.Options{Icon: iconPNG}))
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "MyGo Translate", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			{Label: "检查更新…", Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))

	a := &app{
		manager:     services.NewManager(),
		cfg:         config.Get(),
		from:        fromLangs[0].label,
		to:          toLangs[0].label,
		results:     map[string]*services.ServiceResult{},
		capturePath: *capture,
	}
	if a.cfg.DefaultTargetLang != "" {
		for _, l := range toLangs {
			if l.code == a.cfg.DefaultTargetLang {
				a.to = l.label
			}
		}
	}
	setLang(fromLangs, *from, &a.from)
	setLang(toLangs, *to, &a.to)
	a.manualTarget = *to != ""

	mygo.App.SetName("MyGo Translate")
	applyTheme(a.cfg.Theme)
	mygo.App.WhenReady(func() {
		if err := mygo.App.Dock.SetIcon(iconPNG); err != nil {
			log.Printf("dock icon: %v", err)
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "MyGo Translate",
			Width:     720,
			Height:    680,
			MinWidth:  520,
			MinHeight: 420,
			Content:   ui.View(a.view),
		})
		if err := a.win.SetIcon(iconPNG); err != nil {
			log.Printf("window icon: %v", err)
		}
		a.win.SetAlwaysOnTop(a.cfg.AlwaysOnTop)
		if *query != "" {
			a.query = *query
			a.search(*query)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func labelsOf(ls []lang) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.label
	}
	return out
}

func codeOf(ls []lang, label string) string {
	for _, l := range ls {
		if l.label == label {
			return l.code
		}
	}
	return ls[0].code
}

// setLang picks the label of code in ls, leaving target as it is for an
// empty or unknown code.
func setLang(ls []lang, code string, target *string) {
	if code == "" {
		return
	}
	for _, l := range ls {
		if l.code == code {
			*target = l.label
			return
		}
	}
}

func (a *app) fromCode() string { return codeOf(fromLangs, a.from) }
func (a *app) toCode() string   { return codeOf(toLangs, a.to) }

// activeServices mirrors the web UI: only services that can answer are
// queried, AI and DeepL needing a key, a local endpoint being enough for AI.
func (a *app) activeServices() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range a.cfg.EnabledServices {
		if seen[id] {
			continue
		}
		switch id {
		case "ai":
			if !a.cfg.AIEnabled {
				continue
			}
			if strings.TrimSpace(a.cfg.AIAPIKey) == "" && !isLocalURL(a.cfg.AIBaseURL) {
				continue
			}
		case "deepl":
			if !a.cfg.DeepLEnabled || strings.TrimSpace(a.cfg.DeepLAuthKey) == "" {
				continue
			}
		case "libretranslate":
			if strings.TrimSpace(a.cfg.LibreTranslateURL) == "" {
				continue
			}
		case "youdao", "bing", "google":
		default:
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func (a *app) nameOf(id string) string {
	if p := a.manager.GetProvider(id); p != nil {
		return p.Name()
	}
	return id
}

// applyTheme follows the saved appearance choice; "auto" follows the system.
func applyTheme(theme string) {
	switch theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// settingsState is the copy of the settings the dialog edits: nothing takes
// effect until 保存.
type settingsState struct {
	open   bool
	youdao bool
	bing   bool
	google bool
	ai     bool
	deepl  bool

	target string // label in toLangs
	theme  string // label in themeLangs
	top    bool

	aiBase    string
	aiKey     string
	aiModel   string
	deeplKey  string
	deeplFree bool
	libre     bool
	libreURL  string
}

func (a *app) openSettings() {
	enabled := map[string]bool{}
	for _, id := range a.cfg.EnabledServices {
		enabled[id] = true
	}
	target := toLangs[0].label
	setLang(toLangs, a.cfg.DefaultTargetLang, &target)
	theme := themeLangs[0].label
	setLang(themeLangs, a.cfg.Theme, &theme)
	a.st = settingsState{
		open:      true,
		youdao:    enabled["youdao"],
		bing:      enabled["bing"],
		google:    enabled["google"],
		ai:        a.cfg.AIEnabled,
		deepl:     a.cfg.DeepLEnabled,
		target:    target,
		theme:     theme,
		top:       a.cfg.AlwaysOnTop,
		aiBase:    a.cfg.AIBaseURL,
		aiKey:     a.cfg.AIAPIKey,
		aiModel:   a.cfg.AIModel,
		deeplKey:  a.cfg.DeepLAuthKey,
		deeplFree: a.cfg.DeepLIsFree,
		libre:     enabled["libretranslate"],
		libreURL:  a.cfg.LibreTranslateURL,
	}
}

// saveSettings writes the dialog's copy into the config and applies what the
// open window shows.
func (a *app) saveSettings() error {
	ids := make([]string, 0, 6)
	for _, svc := range []struct {
		on bool
		id string
	}{
		{a.st.youdao, "youdao"},
		{a.st.bing, "bing"},
		{a.st.google, "google"},
		{a.st.ai, "ai"},
		{a.st.deepl, "deepl"},
		{a.st.libre, "libretranslate"},
	} {
		if svc.on {
			ids = append(ids, svc.id)
		}
	}
	if len(ids) == 0 {
		return errors.New("至少启用一个翻译服务")
	}
	a.cfg.EnabledServices = ids
	a.cfg.AIEnabled = a.st.ai
	a.cfg.DeepLEnabled = a.st.deepl
	a.cfg.AIBaseURL = strings.TrimSpace(a.st.aiBase)
	a.cfg.AIAPIKey = strings.TrimSpace(a.st.aiKey)
	a.cfg.AIModel = strings.TrimSpace(a.st.aiModel)
	a.cfg.DeepLAuthKey = strings.TrimSpace(a.st.deeplKey)
	a.cfg.DeepLIsFree = a.st.deeplFree
	a.cfg.LibreTranslateURL = strings.TrimSpace(a.st.libreURL)
	if target := codeOf(toLangs, a.st.target); target != a.cfg.DefaultTargetLang {
		a.to = a.st.target
		a.manualTarget = false
		a.cfg.DefaultTargetLang = target
	}
	a.cfg.Theme = codeOf(themeLangs, a.st.theme)
	a.cfg.AlwaysOnTop = a.st.top
	if err := a.cfg.Save(); err != nil {
		return err
	}
	applyTheme(a.cfg.Theme)
	if a.win != nil {
		a.win.SetAlwaysOnTop(a.cfg.AlwaysOnTop)
	}
	a.st.open = false
	if strings.TrimSpace(a.query) != "" {
		a.search(a.query)
	}
	return nil
}

func (a *app) search(text string) {
	text = strings.TrimSpace(text)
	a.gen++
	gen := a.gen
	if text == "" {
		a.ids = nil
		a.results = map[string]*services.ServiceResult{}
		return
	}

	ids := a.activeServices()
	a.ids = ids
	a.tab = 0
	a.results = make(map[string]*services.ServiceResult, len(ids))
	for _, id := range ids {
		a.results[id] = &services.ServiceResult{ServiceID: id, ServiceName: a.nameOf(id), Status: "loading"}
	}

	a.request = services.QueryRequest{Text: text, FromLang: services.Language(a.fromCode()), ToLang: a.targetFor(text)}
	for _, id := range ids {
		a.queryOne(id, gen, a.request)
	}
}

// queryOne asks one service, in the background, and puts its answer in the
// frame it belongs to.
func (a *app) queryOne(id string, gen int, req services.QueryRequest) {
	go func() {
		res := a.manager.QuerySingle(id, req)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if gen != a.gen {
				return
			}
			if res == nil {
				res = &services.ServiceResult{ServiceID: id, ServiceName: a.nameOf(id), Status: "error", Error: "没有返回结果"}
			}
			a.results[id] = res
			a.maybeCapture()
		})
	}()
}

// targetFor uses the configured direction again for each new query.
func (a *app) targetFor(text string) services.Language {
	target := services.Language(a.toCode())
	if a.manualTarget || a.fromCode() != "auto" {
		return target
	}
	detected := services.DetectLanguage(text)
	switch {
	case detected == services.LangZh && (target == services.LangZh || target == services.LangZhTw):
		return services.LangEn
	}
	return target
}

func (a *app) refresh(id string) {
	res, ok := a.results[id]
	if !ok || res.Status == "loading" {
		return
	}
	a.results[id] = &services.ServiceResult{ServiceID: id, ServiceName: a.nameOf(id), Status: "loading"}
	a.queryOne(id, a.gen, a.request)
}

func (a *app) cancel() {
	a.gen++
	for id, res := range a.results {
		if res.Status == "loading" {
			a.results[id] = &services.ServiceResult{ServiceID: id, ServiceName: res.ServiceName, Status: "cancelled"}
		}
	}
}

func (a *app) busy() bool {
	for _, res := range a.results {
		if res.Status == "loading" {
			return true
		}
	}
	return false
}

func (a *app) swap() {
	from, to := a.fromCode(), string(a.targetFor(a.query))
	if from == "auto" {
		from = string(services.DetectLanguage(a.query))
		if from == "auto" || from == to {
			from = "en"
			if to == "en" {
				from = "zh-CN"
			}
		}
	}
	setLang(fromLangs, to, &a.from)
	setLang(toLangs, from, &a.to)
	a.manualTarget = true
	if strings.TrimSpace(a.query) != "" {
		a.search(a.query)
	}
}

func (a *app) paste() {
	text := strings.TrimSpace(mygo.Clipboard.ReadText())
	if text == "" {
		return
	}
	a.query = text
	a.search(text)
}

func (a *app) speak(url string) {
	if url == "" {
		return
	}
	go func() {
		client := &http.Client{Timeout: 6 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return
		}
		player, err := audio.OpenStream(bytes.NewReader(data))
		if err != nil {
			return
		}
		a.win.Update(func() {
			if a.player != nil {
				a.player.Stop()
			}
			a.player = player
		})
		player.Play()
	}()
}

func (a *app) speakText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	args := []string{}
	if services.DetectLanguage(text) == services.LangZh {
		args = append(args, "-v", "Tingting")
	}
	args = append(args, text)
	cmd := exec.Command("say", args...)
	if a.say != nil && a.say.Process != nil {
		_ = a.say.Process.Kill()
	}
	a.say = cmd
	if a.player != nil {
		a.player.Stop()
		a.player = nil
	}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// maybeCapture writes the screenshot asked for on the command line once the
// answers are in, so that the same window the app shows is the one looked at.
func (a *app) maybeCapture() {
	if a.capturePath == "" || a.captured || a.busy() {
		return
	}
	a.captured = true
	go func() {
		time.Sleep(400 * time.Millisecond)
		png, err := a.win.CapturePage()
		if err != nil {
			log.Printf("capture: %v", err)
			mygo.App.Quit()
			return
		}
		if err := os.WriteFile(a.capturePath, png, 0o644); err != nil {
			log.Printf("capture: %v", err)
		}
		mygo.App.Quit()
	}()
}

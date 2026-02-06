package theme

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"pkt.systems/pslog/ansi"
)

// Theme describes a UI theme suitable for web and mobile clients.
type Theme struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Tokens Tokens `json:"tokens"`
}

// Tokens contains CSS-friendly color tokens for UI chrome.
type Tokens struct {
	Bg               string `json:"bg"`
	Panel            string `json:"panel"`
	PanelAlt         string `json:"panel_alt"`
	Border           string `json:"border"`
	Text             string `json:"text"`
	TextMuted        string `json:"text_muted"`
	TextStrong       string `json:"text_strong"`
	Accent           string `json:"accent"`
	AccentAlt        string `json:"accent_alt"`
	Success          string `json:"success"`
	Warning          string `json:"warning"`
	Error            string `json:"error"`
	Info             string `json:"info"`
	Focus            string `json:"focus"`
	Shadow           string `json:"shadow"`
	Overlay          string `json:"overlay"`
	DialogBg         string `json:"dialog_bg"`
	DialogFg         string `json:"dialog_fg"`
	BannerErrorBg    string `json:"banner_error_bg"`
	BannerErrorFg    string `json:"banner_error_fg"`
	BannerOkBg       string `json:"banner_ok_bg"`
	BannerOkFg       string `json:"banner_ok_fg"`
	TabBg            string `json:"tab_bg"`
	TabFg            string `json:"tab_fg"`
	TabMutedFg       string `json:"tab_muted_fg"`
	TabMutedActiveFg string `json:"tab_muted_active_fg"`
	TabActiveBg      string `json:"tab_active_bg"`
	TabActiveFg      string `json:"tab_active_fg"`
}

// TUITheme contains ANSI sequences for terminal overlay chrome.
type TUITheme struct {
	DialogBg         string
	DialogFg         string
	BannerErrorBg    string
	BannerErrorFg    string
	BannerOkBg       string
	BannerOkFg       string
	TabBg            string
	TabFg            string
	TabMutedFg       string
	TabMutedActiveFg string
	TabActiveBg      string
	TabActiveFg      string
	Reset            string
}

type colorTokens struct {
	bg               Color
	panel            Color
	panelAlt         Color
	border           Color
	text             Color
	textMuted        Color
	textStrong       Color
	accent           Color
	accentAlt        Color
	success          Color
	warning          Color
	err              Color
	info             Color
	focus            Color
	dialogBg         Color
	dialogFg         Color
	bannerErrorBg    Color
	bannerErrorFg    Color
	bannerOkBg       Color
	bannerOkFg       Color
	tabBg            Color
	tabFg            Color
	tabMutedFg       Color
	tabMutedActiveFg Color
	tabActiveBg      Color
	tabActiveFg      Color
}

// Color represents an RGB color.
type Color struct {
	R uint8
	G uint8
	B uint8
}

// Hex returns a CSS hex color string.
func (c Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// ANSIFg returns a truecolor ANSI foreground sequence.
func (c Color) ANSIFg() string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
}

// ANSIBg returns a truecolor ANSI background sequence.
func (c Color) ANSIBg() string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
}

func rgba(c Color, alpha float64) string {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %.2f)", c.R, c.G, c.B, alpha)
}

func mix(a, b Color, t float64) Color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return Color{
		R: uint8(math.Round(float64(a.R)*(1-t) + float64(b.R)*t)),
		G: uint8(math.Round(float64(a.G)*(1-t) + float64(b.G)*t)),
		B: uint8(math.Round(float64(a.B)*(1-t) + float64(b.B)*t)),
	}
}

func lighten(c Color, t float64) Color {
	return mix(c, Color{255, 255, 255}, t)
}

func darken(c Color, t float64) Color {
	return mix(c, Color{0, 0, 0}, t)
}

func contrastRatio(a, b Color) float64 {
	la := luminance(a)
	lb := luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c Color) float64 {
	r := linearizeChannel(c.R)
	g := linearizeChannel(c.G)
	b := linearizeChannel(c.B)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func linearizeChannel(v uint8) float64 {
	s := float64(v) / 255
	if s <= 0.03928 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

func ensureContrast(fg, bg Color, min float64) Color {
	if contrastRatio(fg, bg) >= min {
		return fg
	}
	candidates := []Color{
		lighten(fg, 0.25),
		darken(fg, 0.25),
		Color{255, 255, 255},
		Color{0, 0, 0},
	}
	best := fg
	bestRatio := contrastRatio(fg, bg)
	for _, candidate := range candidates {
		ratio := contrastRatio(candidate, bg)
		if ratio > bestRatio {
			bestRatio = ratio
			best = candidate
		}
	}
	return best
}

type optionColor struct {
	color Color
	ok    bool
}

func opt(c Color, ok bool) optionColor {
	return optionColor{color: c, ok: ok}
}

func pick(colors ...optionColor) Color {
	for _, c := range colors {
		if c.ok {
			return c.color
		}
	}
	return Color{229, 233, 242}
}

func tokensFromColors(c colorTokens) Tokens {
	return Tokens{
		Bg:               c.bg.Hex(),
		Panel:            c.panel.Hex(),
		PanelAlt:         c.panelAlt.Hex(),
		Border:           c.border.Hex(),
		Text:             c.text.Hex(),
		TextMuted:        c.textMuted.Hex(),
		TextStrong:       c.textStrong.Hex(),
		Accent:           c.accent.Hex(),
		AccentAlt:        c.accentAlt.Hex(),
		Success:          c.success.Hex(),
		Warning:          c.warning.Hex(),
		Error:            c.err.Hex(),
		Info:             c.info.Hex(),
		Focus:            c.focus.Hex(),
		Shadow:           rgba(Color{0, 0, 0}, 0.4),
		Overlay:          rgba(Color{0, 0, 0}, 0.55),
		DialogBg:         c.dialogBg.Hex(),
		DialogFg:         c.dialogFg.Hex(),
		BannerErrorBg:    c.bannerErrorBg.Hex(),
		BannerErrorFg:    c.bannerErrorFg.Hex(),
		BannerOkBg:       c.bannerOkBg.Hex(),
		BannerOkFg:       c.bannerOkFg.Hex(),
		TabBg:            c.tabBg.Hex(),
		TabFg:            c.tabFg.Hex(),
		TabMutedFg:       c.tabMutedFg.Hex(),
		TabMutedActiveFg: c.tabMutedActiveFg.Hex(),
		TabActiveBg:      c.tabActiveBg.Hex(),
		TabActiveFg:      c.tabActiveFg.Hex(),
	}
}

func tuiFromColors(c colorTokens) TUITheme {
	return TUITheme{
		DialogBg:         c.dialogBg.ANSIBg(),
		DialogFg:         c.dialogFg.ANSIFg(),
		BannerErrorBg:    c.bannerErrorBg.ANSIBg(),
		BannerErrorFg:    c.bannerErrorFg.ANSIFg(),
		BannerOkBg:       c.bannerOkBg.ANSIBg(),
		BannerOkFg:       c.bannerOkFg.ANSIFg(),
		TabBg:            c.tabBg.ANSIBg(),
		TabFg:            c.tabFg.ANSIFg(),
		TabMutedFg:       c.tabMutedFg.ANSIFg(),
		TabMutedActiveFg: c.tabMutedActiveFg.ANSIFg(),
		TabActiveBg:      c.tabActiveBg.ANSIBg(),
		TabActiveFg:      c.tabActiveFg.ANSIFg(),
		Reset:            "\x1b[0m",
	}
}

func parsePalette(p ansi.Palette) colorTokens {
	key, keyOK := ansiColor(p.Key)
	str, strOK := ansiColor(p.String)
	num, numOK := ansiColor(p.Num)
	info, infoOK := ansiColor(p.Info)
	warn, warnOK := ansiColor(p.Warn)
	err, errOK := ansiColor(p.Error)
	trace, traceOK := ansiColor(p.Trace)
	debug, debugOK := ansiColor(p.Debug)
	msg, msgOK := ansiColor(p.Message)
	msgKey, msgKeyOK := ansiColor(p.MessageKey)
	accent := pick(opt(key, keyOK), opt(info, infoOK))
	accentAlt := pick(opt(str, strOK), opt(num, numOK), opt(accent, true))
	textCandidate := pick(opt(msg, msgOK), opt(str, strOK), opt(key, keyOK), opt(Color{229, 233, 242}, true))
	textStrongCandidate := pick(opt(msgKey, msgKeyOK), opt(accent, true))
	success := pick(opt(info, infoOK), opt(accent, true))
	warning := pick(opt(warn, warnOK), opt(accentAlt, true))
	errColor := pick(opt(err, errOK), opt(warning, true))
	infoColor := pick(opt(trace, traceOK), opt(debug, debugOK), opt(accentAlt, true))

	baseHue := pick(opt(trace, traceOK), opt(debug, debugOK), opt(accent, true))
	bg := darken(baseHue, 0.82)
	panel := lighten(bg, 0.08)
	panelAlt := lighten(bg, 0.15)
	border := lighten(bg, 0.22)
	focus := accent

	text := ensureContrast(textCandidate, bg, 4.5)
	textStrong := ensureContrast(textStrongCandidate, bg, 4.5)
	textMuted := mix(text, bg, 0.45)
	if contrastRatio(textMuted, bg) < 3.0 {
		textMuted = text
	}

	dialogBg := panelAlt
	dialogFg := ensureContrast(text, dialogBg, 4.5)
	bannerErrorBg := errColor
	bannerErrorFg := ensureContrast(textStrong, bannerErrorBg, 4.5)
	bannerOkBg := success
	bannerOkFg := ensureContrast(textStrong, bannerOkBg, 4.5)
	tabBg := mix(accent, bg, 0.55)
	tabFg := ensureContrast(textStrong, tabBg, 4.5)
	tabActiveBg := mix(accentAlt, bg, 0.35)
	tabActiveFg := tabFg
	if contrastRatio(tabActiveFg, tabActiveBg) < 3.0 {
		tabActiveFg = ensureContrast(tabActiveFg, tabActiveBg, 3.0)
	}
	tabMutedCandidate := darkestColor(bg, panel, panelAlt, border, tabBg, tabActiveBg)
	tabMutedFg := adaptTabMutedFg(tabMutedCandidate, textMuted, tabFg, tabBg)
	tabMutedActiveFg := adaptTabMutedFg(tabMutedCandidate, textMuted, tabActiveFg, tabActiveBg)

	return colorTokens{
		bg:               bg,
		panel:            panel,
		panelAlt:         panelAlt,
		border:           border,
		text:             text,
		textMuted:        textMuted,
		textStrong:       textStrong,
		accent:           accent,
		accentAlt:        accentAlt,
		success:          success,
		warning:          warning,
		err:              errColor,
		info:             infoColor,
		focus:            focus,
		dialogBg:         dialogBg,
		dialogFg:         dialogFg,
		bannerErrorBg:    bannerErrorBg,
		bannerErrorFg:    bannerErrorFg,
		bannerOkBg:       bannerOkBg,
		bannerOkFg:       bannerOkFg,
		tabBg:            tabBg,
		tabFg:            tabFg,
		tabMutedFg:       tabMutedFg,
		tabMutedActiveFg: tabMutedActiveFg,
		tabActiveBg:      tabActiveBg,
		tabActiveFg:      tabActiveFg,
	}
}

func darkestColor(colors ...Color) Color {
	if len(colors) == 0 {
		return Color{}
	}
	darkest := colors[0]
	darkestLum := luminance(darkest)
	for i := 1; i < len(colors); i++ {
		lum := luminance(colors[i])
		if lum < darkestLum {
			darkest = colors[i]
			darkestLum = lum
		}
	}
	return darkest
}

func colorDistance(a, b Color) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func adaptTabMutedFg(darkest, textMuted, normalFg, bg Color) Color {
	const minBgContrast = 3.0
	const minFgSeparation = 1.10
	const minFgDistance = 36.0
	const steps = 24

	candidates := make([]Color, 0, 140)
	addRamp := func(a, b Color) {
		for step := 0; step <= steps; step++ {
			t := float64(step) / float64(steps)
			candidates = append(candidates, mix(a, b, t))
		}
	}
	addRamp(darkest, textMuted)
	addRamp(normalFg, darkest)
	addRamp(normalFg, textMuted)
	addRamp(normalFg, bg)
	addRamp(textMuted, bg)
	candidates = append(candidates,
		darkest,
		textMuted,
		normalFg,
		darken(normalFg, 0.35),
		darken(normalFg, 0.5),
		lighten(normalFg, 0.2),
		lighten(normalFg, 0.35),
		Color{0, 0, 0},
		Color{255, 255, 255},
	)

	targetLum := mutedTargetLuminance(normalFg, textMuted, bg)

	type candidateScore struct {
		color        Color
		bgContrast   float64
		fgSeparation float64
		fgDistance   float64
		lum          float64
		lumDelta     float64
		valid        bool
	}
	betterOverall := func(a, b candidateScore) bool {
		if !a.valid {
			return false
		}
		if !b.valid {
			return true
		}
		if a.bgContrast != b.bgContrast {
			return a.bgContrast > b.bgContrast
		}
		if a.fgSeparation != b.fgSeparation {
			return a.fgSeparation > b.fgSeparation
		}
		return a.lum < b.lum
	}
	betterReadable := func(a, b candidateScore) bool {
		if !a.valid {
			return false
		}
		if !b.valid {
			return true
		}
		if a.lumDelta != b.lumDelta {
			return a.lumDelta < b.lumDelta
		}
		if a.fgSeparation != b.fgSeparation {
			return a.fgSeparation > b.fgSeparation
		}
		if a.fgDistance != b.fgDistance {
			return a.fgDistance > b.fgDistance
		}
		if a.bgContrast != b.bgContrast {
			return a.bgContrast > b.bgContrast
		}
		return a.lum < b.lum
	}
	scoreOf := func(c Color) candidateScore {
		return candidateScore{
			color:        c,
			bgContrast:   contrastRatio(c, bg),
			fgSeparation: contrastRatio(c, normalFg),
			fgDistance:   colorDistance(c, normalFg),
			lum:          luminance(c),
			lumDelta:     math.Abs(luminance(c) - targetLum),
			valid:        true,
		}
	}

	bestReadableDistinct := candidateScore{}
	bestReadable := candidateScore{}
	bestOverall := candidateScore{}
	for _, candidate := range candidates {
		score := scoreOf(candidate)
		if betterOverall(score, bestOverall) {
			bestOverall = score
		}
		if score.bgContrast >= minBgContrast {
			if betterReadable(score, bestReadable) {
				bestReadable = score
			}
			if score.fgSeparation >= minFgSeparation && score.fgDistance >= minFgDistance && betterReadable(score, bestReadableDistinct) {
				bestReadableDistinct = score
			}
		}
	}
	if bestReadableDistinct.valid {
		return bestReadableDistinct.color
	}
	if bestReadable.valid {
		return bestReadable.color
	}
	if bestOverall.valid {
		return bestOverall.color
	}
	return textMuted
}

func mutedTargetLuminance(normalFg, textMuted, bg Color) float64 {
	normalLum := luminance(normalFg)
	mutedLum := luminance(textMuted)
	bgLum := luminance(bg)

	target := mutedLum
	switch {
	case normalLum >= 0.75:
		target = math.Min(mutedLum, 0.58)
	case normalLum <= 0.2:
		target = math.Max(mutedLum, normalLum+0.18)
	}
	if bgLum > 0.6 && target < 0.24 {
		target = 0.24
	}
	if target < 0.12 {
		target = 0.12
	}
	if target > 0.72 {
		target = 0.72
	}
	return target
}

type paletteDef struct {
	name    string
	label   string
	palette ansi.Palette
}

var palettes = []paletteDef{
	{name: "default", label: "Default", palette: ansi.PaletteDefault},
	{name: "outrun-electric", label: "Outrun Electric", palette: ansi.PaletteOutrunElectric},
	{name: "doom-iosvkem", label: "Doom Iosvkem", palette: ansi.PaletteDoomIosvkem},
	{name: "doom-gruvbox", label: "Doom Gruvbox", palette: ansi.PaletteDoomGruvbox},
	{name: "doom-dracula", label: "Doom Dracula", palette: ansi.PaletteDoomDracula},
	{name: "doom-nord", label: "Doom Nord", palette: ansi.PaletteDoomNord},
	{name: "tokyo-night", label: "Tokyo Night", palette: ansi.PaletteTokyoNight},
	{name: "solarized-nightfall", label: "Solarized Nightfall", palette: ansi.PaletteSolarizedNightfall},
	{name: "catppuccin-mocha", label: "Catppuccin Mocha", palette: ansi.PaletteCatppuccinMocha},
	{name: "gruvbox-light", label: "Gruvbox Light", palette: ansi.PaletteGruvboxLight},
	{name: "monokai-vibrant", label: "Monokai Vibrant", palette: ansi.PaletteMonokaiVibrant},
	{name: "one-dark-aurora", label: "One Dark Aurora", palette: ansi.PaletteOneDarkAurora},
	{name: "synthwave-84", label: "Synthwave 84", palette: ansi.PaletteSynthwave84},
}

// All returns all palette-derived themes.
func All() []Theme {
	out := make([]Theme, 0, len(palettes))
	for _, def := range palettes {
		colors := parsePalette(def.palette)
		out = append(out, Theme{
			Name:   def.name,
			Label:  def.label,
			Tokens: tokensFromColors(colors),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Label < out[j].Label
	})
	return out
}

// Names returns the names of all themes.
func Names() []string {
	all := All()
	names := make([]string, 0, len(all))
	for _, item := range all {
		if item.Name == "" {
			continue
		}
		names = append(names, item.Name)
	}
	return names
}

// Default returns the default theme.
func Default() Theme {
	for _, def := range palettes {
		if def.name == "default" {
			colors := parsePalette(def.palette)
			return Theme{
				Name:   def.name,
				Label:  def.label,
				Tokens: tokensFromColors(colors),
			}
		}
	}
	out := All()
	if len(out) == 0 {
		return Theme{Name: "default", Label: "Default", Tokens: Tokens{}}
	}
	return out[0]
}

// TUI returns the TUI theme for the named palette.
func TUI(name string) TUITheme {
	def := palettes[0]
	for _, p := range palettes {
		if p.name == name {
			def = p
			break
		}
	}
	colors := parsePalette(def.palette)
	return tuiFromColors(colors)
}

func ansiColor(value string) (Color, bool) {
	return parseANSIColor(value)
}

func parseANSIColor(value string) (Color, bool) {
	trim := strings.TrimSpace(value)
	if trim == "" {
		return Color{}, false
	}
	start := strings.Index(trim, "[")
	end := strings.LastIndex(trim, "m")
	if start == -1 || end == -1 || end <= start {
		return Color{}, false
	}
	codes := strings.Split(trim[start+1:end], ";")
	var fg *Color
	for i := 0; i < len(codes); i++ {
		code, err := strconv.Atoi(codes[i])
		if err != nil {
			continue
		}
		switch {
		case code == 38 && i+2 < len(codes):
			mode, _ := strconv.Atoi(codes[i+1])
			if mode == 5 {
				idx, _ := strconv.Atoi(codes[i+2])
				c := colorFromIndex(idx)
				fg = &c
				i += 2
			}
			if mode == 2 && i+4 < len(codes) {
				r, _ := strconv.Atoi(codes[i+2])
				g, _ := strconv.Atoi(codes[i+3])
				b, _ := strconv.Atoi(codes[i+4])
				c := Color{uint8(r), uint8(g), uint8(b)}
				fg = &c
				i += 4
			}
		case code >= 30 && code <= 37:
			c := colorFromIndex(code - 30)
			fg = &c
		case code >= 90 && code <= 97:
			c := colorFromIndex(code - 90 + 8)
			fg = &c
		}
	}
	if fg == nil {
		return Color{}, false
	}
	return *fg, true
}

func colorFromIndex(idx int) Color {
	switch idx {
	case 0:
		return Color{0, 0, 0}
	case 1:
		return Color{128, 0, 0}
	case 2:
		return Color{0, 128, 0}
	case 3:
		return Color{128, 128, 0}
	case 4:
		return Color{0, 0, 128}
	case 5:
		return Color{128, 0, 128}
	case 6:
		return Color{0, 128, 128}
	case 7:
		return Color{192, 192, 192}
	case 8:
		return Color{128, 128, 128}
	case 9:
		return Color{255, 0, 0}
	case 10:
		return Color{0, 255, 0}
	case 11:
		return Color{255, 255, 0}
	case 12:
		return Color{0, 0, 255}
	case 13:
		return Color{255, 0, 255}
	case 14:
		return Color{0, 255, 255}
	case 15:
		return Color{255, 255, 255}
	}
	if idx >= 16 && idx <= 231 {
		idx -= 16
		r := idx / 36
		g := (idx / 6) % 6
		b := idx % 6
		return Color{
			R: cubeValue(r),
			G: cubeValue(g),
			B: cubeValue(b),
		}
	}
	if idx >= 232 && idx <= 255 {
		level := uint8(8 + (idx-232)*10)
		return Color{level, level, level}
	}
	return Color{229, 233, 242}
}

func cubeValue(v int) uint8 {
	switch v {
	case 0:
		return 0
	case 1:
		return 95
	case 2:
		return 135
	case 3:
		return 175
	case 4:
		return 215
	default:
		return 255
	}
}

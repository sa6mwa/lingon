package grapheme

const (
	ansiReset   = "\x1b[0m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiBold    = "\x1b[1m"
)

// Sample describes a grapheme test sample.
type Sample struct {
	Name        string
	Description string
	Lines       []string
}

// Samples returns the canonical grapheme samples for tests and CLI output.
func Samples() []Sample {
	return []Sample{
		{
			Name:        "Color basics",
			Description: "ANSI colors mixed with symbols",
			Lines: []string{
				ansiGreen + "OK" + ansiReset + " " + ansiRed + "FAIL" + ansiReset + " " + ansiYellow + "WARN" + ansiReset,
				ansiCyan + "cyan" + ansiReset + " " + ansiMagenta + "magenta" + ansiReset + " " + ansiBlue + "blue" + ansiReset,
			},
		},
		{
			Name:        "Combining marks",
			Description: "Base + combining accents",
			Lines: []string{
				"e\u0301 cafe\u0301 na\u0303ve co\u0308te",
			},
		},
		{
			Name:        "Variation selectors",
			Description: "Text vs emoji presentation",
			Lines: []string{
				"✖︎ ✖️ ❌️ ✅ ☑️",
			},
		},
		{
			Name:        "ZWJ sequences",
			Description: "Family and occupation emoji",
			Lines: []string{
				"👨‍👩‍👧‍👦 👩‍💻 👨‍🚀 👩‍🚀",
			},
		},
		{
			Name:        "Skin tone modifiers",
			Description: "Emoji with skin tone",
			Lines: []string{
				"👍🏻 👍🏽 👍🏿 👋🏾",
			},
		},
		{
			Name:        "Flags",
			Description: "Regional indicator flags",
			Lines: []string{
				"🇺🇸 🇸🇪 🇯🇵 🇩🇪",
			},
		},
		{
			Name:        "Keycaps",
			Description: "Keycap sequences",
			Lines: []string{
				"1️⃣ 2️⃣ 3️⃣ 4️⃣ 5️⃣",
			},
		},
		{
			Name:        "Japanese",
			Description: "CJK wide characters",
			Lines: []string{
				"日本語のテキストです",
				"漢字かなカナ混在",
			},
		},
		{
			Name:        "Mixed scripts",
			Description: "Latin + CJK + symbols",
			Lines: []string{
				"ABC 漢字 かな カナ 123",
			},
		},
		{
			Name:        "Symbols",
			Description: "Common symbols and punctuation",
			Lines: []string{
				"✓ ✗ © ™ ∞ — • …",
			},
		},
		{
			Name:        "Cursor alignment",
			Description: "Wide grapheme followed by spaces",
			Lines: []string{
				"1 ❌️  X",
				ansiBold + "2" + ansiReset + " ❌️  Y",
			},
		},
	}
}

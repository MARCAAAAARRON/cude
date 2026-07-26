package tui

// Pixel-art ASCII block icons — replaces all Unicode emojis in the TUI.
// These render cleanly in every terminal without font support issues.
const (
	IconOK    = "[■]" // success / approved
	IconFail  = "[×]" // error / denied / failed
	IconSpark = "[*]" // new / sparkle
	IconBox   = "[□]" // compact / package
	IconFile  = "[≡]" // file / export
	IconEye   = "[◉]" // details / inspect
	IconBrain = "[◈]" // thinking / reasoning
	IconPaint = "[▓]" // theme / paint
	IconCoin  = "[¢]" // cost / money
)

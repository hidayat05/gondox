package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

var (
	//go:embed assets/JetBrainsMono-Regular.ttf
	jetBrainsMonoRegular []byte
	//go:embed assets/JetBrainsMono-Bold.ttf
	jetBrainsMonoBold []byte
	//go:embed assets/JetBrainsMono-Italic.ttf
	jetBrainsMonoItalic []byte
	//go:embed assets/JetBrainsMono-BoldItalic.ttf
	jetBrainsMonoBoldItalic []byte
)

var (
	jetBrainsMonoRegularRes    = fyne.NewStaticResource("JetBrainsMono-Regular.ttf", jetBrainsMonoRegular)
	jetBrainsMonoBoldRes       = fyne.NewStaticResource("JetBrainsMono-Bold.ttf", jetBrainsMonoBold)
	jetBrainsMonoItalicRes     = fyne.NewStaticResource("JetBrainsMono-Italic.ttf", jetBrainsMonoItalic)
	jetBrainsMonoBoldItalicRes = fyne.NewStaticResource("JetBrainsMono-BoldItalic.ttf", jetBrainsMonoBoldItalic)
)

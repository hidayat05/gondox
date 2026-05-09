package ui

import (
	"fmt"
	"gondox/internal/cache"
	"gondox/internal/downloader"
	"gondox/internal/runner"
	"gondox/internal/versions"
	"image/color"
	"io"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type appTheme struct {
	variant fyne.ThemeVariant
}

const (
	lightVariant = fyne.ThemeVariant(0)
	darkVariant  = fyne.ThemeVariant(1)
)

func (t *appTheme) Color(c fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(c, t.variant)
}

func (t *appTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *appTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Bold && style.Italic {
		return jetBrainsMonoBoldItalicRes
	}
	if style.Bold {
		return jetBrainsMonoBoldRes
	}
	if style.Italic {
		return jetBrainsMonoItalicRes
	}
	return jetBrainsMonoRegularRes
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}

func Run() {
	a := app.NewWithID("com.gondox.protogen")
	a.Settings().SetTheme(&appTheme{variant: lightVariant})

	w := a.NewWindow("Gondox — Proto to Go Generator")
	w.Resize(fyne.NewSize(1000, 800))

	termView := widget.NewRichText()
	termView.Wrapping = fyne.TextWrapWord

	tw := newTermWriter()
	tw.startAutoFlush(80 * time.Millisecond)

	log := &consoleLogger{out: tw}

	var (
		protoDir string
		destDir  string
	)

	var themeToggleBtn *widget.Button
	isDark := false
	themeToggleBtn = widget.NewButton("🌙 Dark", func() {
		isDark = !isDark
		if isDark {
			a.Settings().SetTheme(&appTheme{variant: darkVariant})
			themeToggleBtn.SetText("☀️ Light")
		} else {
			a.Settings().SetTheme(&appTheme{variant: lightVariant})
			themeToggleBtn.SetText("🌙 Dark")
		}
	})

	makeBadge := func(name, version string) *widget.Label {
		info := cache.CheckBinary(name, version)
		if info.Exists {
			lbl := widget.NewLabel("✓ Cached")
			lbl.Importance = widget.SuccessImportance
			return lbl
		}
		lbl := widget.NewLabel("○ Not Downloaded")
		lbl.Importance = widget.LowImportance
		return lbl
	}

	protocBadge := container.NewStack(widget.NewLabel(""))
	genGoBadge := container.NewStack(widget.NewLabel(""))
	genGrpcBadge := container.NewStack(widget.NewLabel(""))

	protocSelect := widget.NewSelect([]string{"Loading..."}, nil)
	genGoSelect := widget.NewSelect([]string{"Loading..."}, nil)
	genGrpcSelect := widget.NewSelect([]string{"Loading..."}, nil)

	var (
		protocVersions       []string
		genGoVersions        []string
		genGrpcVersions      []string
		protocDisplayToRaw   = map[string]string{}
		genGoDisplayToRaw    = map[string]string{}
		genGrpcDisplayToRaw  = map[string]string{}
		protocRawToDisplay   = map[string]string{}
		genGoRawToDisplay    = map[string]string{}
		genGrpcRawToDisplay  = map[string]string{}
		suppressSelectChange bool
	)

	protocDownloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), nil)
	genGoDownloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), nil)
	genGrpcDownloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), nil)

	protocSelect.PlaceHolder = "Select version"
	genGoSelect.PlaceHolder = "Select version"
	genGrpcSelect.PlaceHolder = "Select version"

	selectedRaw := func(sel *widget.Select, displayToRaw map[string]string) string {
		selected := sel.Selected
		if selected == "" || selected == "Loading..." {
			return selected
		}
		if raw, ok := displayToRaw[selected]; ok {
			return raw
		}
		return strings.TrimSpace(strings.TrimPrefix(selected, "✓"))
	}

	buildVersionOptions := func(
		name string,
		versions []string,
	) (
		[]string,
		map[string]string,
		map[string]string,
	) {
		opts := make([]string, 0, len(versions))
		displayToRaw := make(map[string]string, len(versions))
		rawToDisplay := make(map[string]string, len(versions))
		for _, v := range versions {
			label := "  " + v
			if cache.CheckBinary(name, v).Exists {
				label = "✓ " + v
			}
			opts = append(opts, label)
			displayToRaw[label] = v
			rawToDisplay[v] = label
		}
		return opts, displayToRaw, rawToDisplay
	}

	applyVersionOptions := func(
		name string,
		sel *widget.Select,
		versions []string,
		displayToRaw *map[string]string,
		rawToDisplay *map[string]string,
	) {
		prev := selectedRaw(sel, *displayToRaw)
		opts, d2r, r2d := buildVersionOptions(name, versions)
		suppressSelectChange = true
		sel.Options = opts
		sel.Refresh()
		*displayToRaw = d2r
		*rawToDisplay = r2d
		if prev != "" && prev != "Loading..." {
			if display, ok := r2d[prev]; ok {
				sel.SetSelected(display)
			}
		}
		if sel.Selected == "" && len(opts) > 0 {
			sel.SetSelected(opts[0])
		}
		suppressSelectChange = false
	}

	refreshSelectorCheckmarks := func() {
		if len(protocVersions) > 0 {
			applyVersionOptions(
				"protoc",
				protocSelect,
				protocVersions,
				&protocDisplayToRaw,
				&protocRawToDisplay,
			)
		}
		if len(genGoVersions) > 0 {
			applyVersionOptions(
				"protoc-gen-go",
				genGoSelect,
				genGoVersions,
				&genGoDisplayToRaw,
				&genGoRawToDisplay,
			)
		}
		if len(genGrpcVersions) > 0 {
			applyVersionOptions(
				"protoc-gen-go-grpc",
				genGrpcSelect,
				genGrpcVersions,
				&genGrpcDisplayToRaw,
				&genGrpcRawToDisplay,
			)
		}
	}

	refreshBadges := func() {
		protocBadge.Objects = []fyne.CanvasObject{makeBadge(
			"protoc",
			selectedRaw(protocSelect, protocDisplayToRaw),
		)}
		genGoBadge.Objects = []fyne.CanvasObject{makeBadge(
			"protoc-gen-go",
			selectedRaw(genGoSelect, genGoDisplayToRaw),
		)}
		genGrpcBadge.Objects = []fyne.CanvasObject{makeBadge(
			"protoc-gen-go-grpc",
			selectedRaw(genGrpcSelect, genGrpcDisplayToRaw),
		)}

		protocBadge.Refresh()
		genGoBadge.Refresh()
		genGrpcBadge.Refresh()
	}

	refreshDownloadButtons := func() {
		type entry struct {
			name string
			sel  *widget.Select
			btn  *widget.Button
		}
		for _, e := range []entry{
			{name: "protoc", sel: protocSelect, btn: protocDownloadBtn},
			{name: "protoc-gen-go", sel: genGoSelect, btn: genGoDownloadBtn},
			{name: "protoc-gen-go-grpc", sel: genGrpcSelect, btn: genGrpcDownloadBtn},
		} {
			var selected string
			switch e.name {
			case "protoc":
				selected = selectedRaw(e.sel, protocDisplayToRaw)
			case "protoc-gen-go":
				selected = selectedRaw(e.sel, genGoDisplayToRaw)
			default:
				selected = selectedRaw(e.sel, genGrpcDisplayToRaw)
			}
			if selected == "" || selected == "Loading..." {
				e.btn.Hide()
				continue
			}
			if cache.CheckBinary(e.name, selected).Exists {
				e.btn.Hide()
			} else {
				e.btn.Show()
				e.btn.Enable()
				e.btn.SetText("Download")
			}
		}
	}

	refreshCompilerStatus := func() {
		refreshBadges()
		refreshDownloadButtons()
	}

	protocSelect.OnChanged = func(_ string) {
		if suppressSelectChange {
			return
		}
		refreshCompilerStatus()
	}

	genGoSelect.OnChanged = func(_ string) {
		if suppressSelectChange {
			return
		}
		refreshCompilerStatus()
	}

	genGrpcSelect.OnChanged = func(_ string) {
		if suppressSelectChange {
			return
		}
		refreshCompilerStatus()
	}

	downloadSelected := func(
		name string,
		sel *widget.Select,
		displayToRaw map[string]string,
		btn *widget.Button,
		fn func(string, downloader.ProgressFunc) (string, error),
	) {
		version := selectedRaw(sel, displayToRaw)
		if version == "" || version == "Loading..." {
			dialog.ShowError(fmt.Errorf("please select a version first"), w)
			return
		}
		btn.Disable()
		btn.SetText("Downloading...")
		log.info("Downloading %s v%s", name, version)
		go func() {
			path, err := fn(version, nil)
			if err != nil {
				log.error("Failed downloading %s v%s: %v", name, version, err)
				btn.Enable()
				btn.SetText("Download")
				return
			}
			log.success("Downloaded %s v%s to %s", name, version, path)
			refreshSelectorCheckmarks()
			refreshCompilerStatus()
		}()
	}

	protocDownloadBtn.OnTapped = func() {
		downloadSelected(
			"protoc",
			protocSelect,
			protocDisplayToRaw,
			protocDownloadBtn,
			downloader.DownloadProtoc,
		)
	}

	genGoDownloadBtn.OnTapped = func() {
		downloadSelected(
			"protoc-gen-go",
			genGoSelect,
			genGoDisplayToRaw,
			genGoDownloadBtn,
			downloader.DownloadProtocGenGo,
		)
	}

	genGrpcDownloadBtn.OnTapped = func() {
		downloadSelected(
			"protoc-gen-go-grpc",
			genGrpcSelect,
			genGrpcDisplayToRaw,
			genGrpcDownloadBtn,
			downloader.DownloadProtocGenGoGRPC,
		)
	}

	protocDownloadBtn.Hide()
	genGoDownloadBtn.Hide()
	genGrpcDownloadBtn.Hide()

	makeVersionBox := func(
		title string,
		sel *widget.Select,
		badge *fyne.Container,
		downloadBtn *widget.Button,
	) *fyne.Container {
		titleLbl := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		return container.NewVBox(
			titleLbl,
			container.NewBorder(nil, nil, nil, downloadBtn, sel),
			badge,
		)
	}

	versionsGrid := container.NewAdaptiveGrid(3,
		makeVersionBox("PROTOC", protocSelect, protocBadge, protocDownloadBtn),
		makeVersionBox("PROTOC-GEN-GO", genGoSelect, genGoBadge, genGoDownloadBtn),
		makeVersionBox("PROTOC-GEN-GO-GRPC", genGrpcSelect, genGrpcBadge, genGrpcDownloadBtn),
	)

	versionCard := widget.NewCard("Compiler Versions", "", versionsGrid)

	protoDirDisplay := widget.NewLabel("~/projects/my-grpc-api/proto/")
	protoDirDisplay.Wrapping = fyne.TextWrapBreak
	protoDirDisplay.TextStyle = fyne.TextStyle{Italic: true}

	destDirDisplay := widget.NewLabel("~/projects/my-grpc-api/gen/go/")
	destDirDisplay.Wrapping = fyne.TextWrapBreak
	destDirDisplay.TextStyle = fyne.TextStyle{Italic: true}

	protoDirBtn := widget.NewButtonWithIcon(
		"Browse",
		theme.FolderOpenIcon(),
		func() {
			dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
				if err != nil || uri == nil {
					return
				}
				protoDir = uri.Path()
				protoDirDisplay.SetText(protoDir)
			}, w)
		},
	)

	destDirBtn := widget.NewButtonWithIcon(
		"Browse",
		theme.FolderOpenIcon(),
		func() {
			dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
				if err != nil || uri == nil {
					return
				}
				destDir = uri.Path()
				destDirDisplay.SetText(destDir)
			}, w)
		},
	)

	sourceSection := container.NewBorder(
		nil, nil, nil, protoDirBtn,
		protoDirDisplay,
	)

	destSection := container.NewBorder(
		nil, nil, nil, destDirBtn,
		destDirDisplay,
	)

	sourceLbl := widget.NewLabelWithStyle("Source", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	sourceLbl.Truncation = fyne.TextTruncateClip

	destLbl := widget.NewLabelWithStyle("Destination", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	destLbl.Truncation = fyne.TextTruncateClip

	dirsGrid := container.NewAdaptiveGrid(2,
		widget.NewCard("", "", container.NewVBox(sourceLbl, sourceSection)),
		widget.NewCard("", "", container.NewVBox(destLbl, destSection)),
	)
	dirCard := widget.NewCard("", "", dirsGrid)

	generateBtn := widget.NewButtonWithIcon("  Generate Go Code", theme.MediaPlayIcon(), nil)
	generateBtn.Importance = widget.HighImportance
	generateBtn.OnTapped = func() {
		if protoDir == "" || destDir == "" {
			dialog.ShowError(fmt.Errorf("please select both .proto source and output directories"), w)
			return
		}
		snapProtoc := selectedRaw(protocSelect, protocDisplayToRaw)
		snapGenGo := selectedRaw(genGoSelect, genGoDisplayToRaw)
		snapGenGrpc := selectedRaw(genGrpcSelect, genGrpcDisplayToRaw)
		if snapProtoc == "" || snapProtoc == "Loading..." ||
			snapGenGo == "" || snapGenGo == "Loading..." ||
			snapGenGrpc == "" || snapGenGrpc == "Loading..." {
			dialog.ShowError(fmt.Errorf("please wait for version list to finish loading"), w)
			return
		}
		protocInfo := cache.CheckBinary("protoc", snapProtoc)
		genGoInfo := cache.CheckBinary("protoc-gen-go", snapGenGo)
		genGrpcInfo := cache.CheckBinary("protoc-gen-go-grpc", snapGenGrpc)
		var missing []string
		if !protocInfo.Exists {
			missing = append(missing, "protoc v"+snapProtoc)
		}
		if !genGoInfo.Exists {
			missing = append(missing, "protoc-gen-go v"+snapGenGo)
		}
		if !genGrpcInfo.Exists {
			missing = append(missing, "protoc-gen-go-grpc v"+snapGenGrpc)
		}
		if len(missing) > 0 {
			dialog.ShowError(fmt.Errorf("binaries not downloaded yet:\n• %s", strings.Join(missing, "\n• ")), w)
			return
		}
		generateBtn.Disable()
		tw.clear()
		go func() {
			defer generateBtn.Enable()
			log.section("Generation Start")
			log.kv("protoc", "v"+snapProtoc)
			log.kv("protoc-gen-go", "v"+snapGenGo)
			log.kv("protoc-gen-go-grpc", "v"+snapGenGrpc)
			log.kv("source", protoDir)
			log.kv("output", destDir)
			if err := runner.Run(runner.RunConfig{
				ProtocPath:        protocInfo.Path,
				ProtocGenGoPath:   genGoInfo.Path,
				ProtocGenGrpcPath: genGrpcInfo.Path,
				ProtoDir:          protoDir,
				DestDir:           destDir,
				Output:            tw,
			}); err != nil {
				log.error("Generation failed: %v", err)
				return
			}
			log.success("Generation completed")
		}()
	}

	cacheDir, _ := cache.CacheDir()
	clearBtn := widget.NewButtonWithIcon("Clear Cache", theme.DeleteIcon(), func() {
		if err := cache.ClearCache(); err != nil {
			dialog.ShowError(fmt.Errorf("failed to clear cache: %v", err), w)
		} else {
			tw.clear()
			log.success("Cache cleared successfully")
			refreshSelectorCheckmarks()
			refreshCompilerStatus()
		}
	})
	clearBtn.Importance = widget.MediumImportance

	cacheLbl := widget.NewLabel(fmt.Sprintf("Cache: %s", cacheDir))
	cacheLbl.TextStyle = fyne.TextStyle{Italic: true}

	termScroll := container.NewScroll(termView)
	termScroll.SetMinSize(fyne.NewSize(0, 250))

	tw.onFlush = func(text string) {
		termView.Segments = buildLogSegments(text)
		termView.Refresh()
		termScroll.ScrollToBottom()
	}

	termCard := widget.NewCard("Output Console", "",
		container.NewBorder(
			nil,
			container.NewHBox(cacheLbl),
			nil, nil,
			termScroll,
		),
	)

	topSection := container.NewVBox(
		versionCard,
		dirCard,
		container.NewCenter(generateBtn),
	)

	headerBar := container.NewBorder(
		nil, nil,
		widget.NewLabelWithStyle(
			"Gondox — Proto to Go Generator",
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		),
		container.NewHBox(themeToggleBtn, clearBtn),
		nil,
	)

	bodySplit := container.NewVSplit(topSection, termCard)
	bodySplit.Offset = 0.58

	mainContent := container.NewBorder(
		container.NewVBox(headerBar, widget.NewSeparator()),
		nil, nil, nil,
		bodySplit,
	)

	w.SetContent(container.NewPadded(mainContent))
	go func() {
		type result struct {
			vers   []string
			err    error
			source string
		}

		chProtoc := make(chan result, 1)
		chGenGo := make(chan result, 1)
		chGrpc := make(chan result, 1)

		log.section("Startup")
		log.info("Fetching version list from GitHub")

		go func() {
			cachedBefore := versions.HasCachedVersionList("protoc")
			v, e := versions.FetchProtocVersions()
			source := "GitHub"
			if cachedBefore || (e != nil && len(v) > 0) {
				source = "cache"
			}
			chProtoc <- result{v, e, source}
		}()

		go func() {
			cachedBefore := versions.HasCachedVersionList("protoc-gen-go")
			v, e := versions.FetchProtocGenGoVersions()
			source := "GitHub"
			if cachedBefore || (e != nil && len(v) > 0) {
				source = "cache"
			}
			chGenGo <- result{v, e, source}
		}()

		go func() {
			cachedBefore := versions.HasCachedVersionList("protoc-gen-go-grpc")
			v, e := versions.FetchProtocGenGoGRPCVersions()
			source := "GitHub"
			if cachedBefore || (e != nil && len(v) > 0) {
				source = "cache"
			}
			chGrpc <- result{v, e, source}
		}()

		if r := <-chProtoc; r.err == nil && len(r.vers) > 0 {
			protocVersions = r.vers
			applyVersionOptions(
				"protoc",
				protocSelect,
				protocVersions,
				&protocDisplayToRaw,
				&protocRawToDisplay,
			)
			log.success("protoc: %d versions found from %s (latest: v%s)", len(r.vers), r.source, r.vers[0])
		} else {
			logVersionFetchError("protoc", r.err, log)
		}

		if r := <-chGenGo; r.err == nil && len(r.vers) > 0 {
			genGoVersions = r.vers
			applyVersionOptions(
				"protoc-gen-go",
				genGoSelect,
				genGoVersions,
				&genGoDisplayToRaw,
				&genGoRawToDisplay,
			)
			log.success("protoc-gen-go: %d versions found from %s (latest: v%s)", len(r.vers), r.source, r.vers[0])
		} else {
			logVersionFetchError("protoc-gen-go", r.err, log)
		}

		if r := <-chGrpc; r.err == nil && len(r.vers) > 0 {
			genGrpcVersions = r.vers
			applyVersionOptions(
				"protoc-gen-go-grpc",
				genGrpcSelect,
				genGrpcVersions,
				&genGrpcDisplayToRaw,
				&genGrpcRawToDisplay,
			)
			log.success("protoc-gen-go-grpc: %d versions found from %s (latest: v%s)", len(r.vers), r.source, r.vers[0])
		} else {
			logVersionFetchError("protoc-gen-go-grpc", r.err, log)
		}

		refreshCompilerStatus()
		log.success("Ready. Select versions, download binaries, choose directories and click Generate")
	}()

	w.ShowAndRun()
}
func logVersionFetchError(name string, err error, log *consoleLogger) {
	if err == nil {
		return
	}

	if versions.IsRateLimitError(err) {
		log.section(fmt.Sprintf("⚠  GITHUB API RATE LIMIT (%s)", name))
		log.warn("GitHub API rate limit reached")
		log.warn("This is a temporary restriction from GitHub (usually recovers in 1 hour)")
		log.warn("Options:")
		log.warn("  • Wait and try again later")
		log.warn("  • Use locally cached compiler versions (if available)")
		log.warn("")
	} else {
		log.warn("Failed to fetch %s versions: %v — using cached versions if available", name, err)
	}
}

type termWriter struct {
	mu      sync.Mutex
	buf     strings.Builder
	onFlush func(string)
	dirty   bool
}

const maxConsoleBytes = 256 * 1024

func newTermWriter() *termWriter {
	return &termWriter{}
}

func (t *termWriter) startAutoFlush(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			t.flush()
		}
	}()
}

func (t *termWriter) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	t.buf.Write(p)
	if t.buf.Len() > maxConsoleBytes {
		s := t.buf.String()
		cut := len(s) - maxConsoleBytes
		if cut > 0 {
			if idx := strings.IndexByte(s[cut:], '\n'); idx >= 0 {
				cut += idx + 1
			}
			t.buf.Reset()
			t.buf.WriteString("[log trimmed to keep UI responsive]\n")
			t.buf.WriteString(s[cut:])
		}
	}
	t.dirty = true
	t.mu.Unlock()
	return len(p), nil
}

func (t *termWriter) clear() {
	t.mu.Lock()
	t.buf.Reset()
	t.dirty = true
	t.mu.Unlock()
	t.flush()
}

func (t *termWriter) flush() {
	t.mu.Lock()
	if !t.dirty {
		t.mu.Unlock()
		return
	}
	text := t.buf.String()
	t.dirty = false
	onFlush := t.onFlush
	t.mu.Unlock()
	if onFlush != nil {
		onFlush(text)
	}
}

func buildLogSegments(text string) []widget.RichTextSegment {
	if text == "" {
		return []widget.RichTextSegment{&widget.TextSegment{
			Text:  "",
			Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Monospace: true}},
		}}
	}

	lines := strings.SplitAfter(text, "\n")
	segments := make([]widget.RichTextSegment, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		segments = append(segments, &widget.TextSegment{
			Text: line,
			Style: widget.RichTextStyle{
				ColorName: logLineColor(line),
				TextStyle: fyne.TextStyle{Monospace: true},
			},
		})
	}
	return segments
}

func logLineColor(line string) fyne.ThemeColorName {
	if strings.Contains(line, "] ERROR ") || strings.Contains(line, "✗") {
		return theme.ColorNameError
	}
	if strings.Contains(line, "] WARN ") {
		return theme.ColorNameWarning
	}
	if strings.Contains(line, "] OK ") || strings.Contains(line, "✓") {
		return theme.ColorNameSuccess
	}
	if strings.Contains(line, "] INFO ") {
		return theme.ColorNamePrimary
	}
	return theme.ColorNameForeground
}

var _ io.Writer = (*termWriter)(nil)

type consoleLogger struct {
	out io.Writer
}

func (l *consoleLogger) info(format string, args ...any) {
	l.printf("INFO", format, args...)
}

func (l *consoleLogger) success(format string, args ...any) {
	l.printf("OK", format, args...)
}

func (l *consoleLogger) warn(format string, args ...any) {
	l.printf("WARN", format, args...)
}

func (l *consoleLogger) error(format string, args ...any) {
	l.printf("ERROR", format, args...)
}

func (l *consoleLogger) kv(key, value string) {
	_, _ = fmt.Fprintf(l.out, "  - %-18s %s\n", key+":", value)
}

func (l *consoleLogger) section(title string) {
	_, _ = fmt.Fprintln(l.out, strings.Repeat("=", 68))
	l.info(title)
}

func (l *consoleLogger) printf(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(l.out, "[%s] %-5s %s\n", time.Now().Format("15:04:05"), level, msg)
}

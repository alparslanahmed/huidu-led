package huidu

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	hd2020RealtimeWidth  = 64
	hd2020RealtimeHeight = 32
)

type hd2020BitmapMode int

const (
	hd2020BitmapLegacyTwoPlane hd2020BitmapMode = iota
	hd2020BitmapFullColorRGB
)

type hd2020SendMode int

const (
	hd2020SendModeRealtime hd2020SendMode = iota
	hd2020SendModeProgram
)

type hd2020TextScreen struct {
	text   string
	width  int
	height int
	config TextConfig
}

func (d *Device) sendHD2020Gen6Screen(screen *Screen) error {
	spec, err := extractHD2020TextScreen(screen)
	if err != nil {
		return err
	}

	bitmapMode := d.hd2020BitmapMode(spec)
	sendMode := d.hd2020SendMode(spec)
	mask, err := renderHD2020TextMask(spec.text, spec.width, spec.height, spec.config)
	if err != nil {
		return err
	}

	transferID := uint16(time.Now().UnixNano())
	if sendMode == hd2020SendModeProgram {
		bitmap, err := encodeHD2020ProgramBitmap(mask, spec.width, spec.height, spec.config.Color, spec.config.BackgroundColor)
		if err != nil {
			return err
		}
		packets := buildHD2020ProgramScreenPackets(bitmap, spec.width, spec.height, transferID)
		return d.sendHD2020Packets(packets)
	}

	bitmap, planes, err := encodeHD2020TextBitmap(mask, spec.width, spec.height, spec.config.Color, spec.config.BackgroundColor, bitmapMode)
	if err != nil {
		return err
	}

	if shouldSendHD2020ScreenSetup(bitmapMode, spec.width, spec.height) {
		setupPackets := buildHD2020RealtimeScreenSetupPackets(spec.width, spec.height, transferID-1)
		if err := d.sendHD2020Packets(setupPackets); err != nil {
			return err
		}
	}

	packets := buildHD2020RealtimeAreaPackets(bitmap, spec.width, spec.height, planes, transferID)
	return d.sendHD2020Packets(packets)
}

func extractHD2020TextScreen(screen *Screen) (hd2020TextScreen, error) {
	if screen == nil {
		return hd2020TextScreen{}, fmt.Errorf("screen nil")
	}

	for _, program := range screen.Programs {
		for _, area := range program.Areas {
			if area.X != 0 || area.Y != 0 {
				return hd2020TextScreen{}, fmt.Errorf("%w: HD2020/Gen6 realtime backend sadece x=0,y=0 alan destekliyor", ErrUnsupportedProtocol)
			}
			for _, item := range area.items {
				text, ok := item.(*textItem)
				if !ok {
					return hd2020TextScreen{}, fmt.Errorf("%w: HD2020/Gen6 realtime backend sadece metin içeriğini bitmap olarak gönderebilir", ErrUnsupportedProtocol)
				}
				return hd2020TextScreen{
					text:   text.text,
					width:  area.Width,
					height: area.Height,
					config: text.config,
				}, nil
			}
		}
	}

	return hd2020TextScreen{}, fmt.Errorf("%w: HD2020/Gen6 için gönderilecek metin alanı bulunamadı", ErrUnsupportedProtocol)
}

func (d *Device) hd2020BitmapMode(spec hd2020TextScreen) hd2020BitmapMode {
	if mode, ok := hd2020BitmapModeOverride(); ok {
		return mode
	}

	d.mu.Lock()
	cardType := d.hd2020CardType
	cardTypeKnown := d.hd2020CardTypeKnown
	d.mu.Unlock()

	if cardTypeKnown && isHD2020FullColorCard(cardType) {
		return hd2020BitmapFullColorRGB
	}
	if spec.width > hd2020RealtimeWidth || spec.height > hd2020RealtimeHeight {
		return hd2020BitmapFullColorRGB
	}
	if colorNeedsHD2020RGB(spec.config.Color) {
		return hd2020BitmapFullColorRGB
	}
	return hd2020BitmapLegacyTwoPlane
}

func (d *Device) hd2020SendMode(spec hd2020TextScreen) hd2020SendMode {
	if mode, ok := hd2020SendModeOverride(); ok {
		return mode
	}
	if spec.width > hd2020RealtimeWidth || spec.height > hd2020RealtimeHeight {
		return hd2020SendModeProgram
	}
	return hd2020SendModeRealtime
}

func hd2020SendModeOverride() (hd2020SendMode, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HUIDU_HD2020_SEND_MODE"))) {
	case "program", "screen", "upload":
		return hd2020SendModeProgram, true
	case "realtime", "rt":
		return hd2020SendModeRealtime, true
	default:
		return hd2020SendModeRealtime, false
	}
}

func hd2020BitmapModeOverride() (hd2020BitmapMode, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HUIDU_HD2020_BITMAP_MODE"))) {
	case "legacy", "2", "2plane", "two-plane":
		return hd2020BitmapLegacyTwoPlane, true
	case "rgb", "full", "fullcolor", "full-color", "3", "3plane":
		return hd2020BitmapFullColorRGB, true
	default:
		return hd2020BitmapLegacyTwoPlane, false
	}
}

func shouldSendHD2020ScreenSetup(mode hd2020BitmapMode, width, height int) bool {
	if !hd2020BoolEnv("HUIDU_HD2020_SKIP_SETUP") {
		if hd2020BoolEnv("HUIDU_HD2020_FORCE_SETUP") {
			return width > hd2020RealtimeWidth || height > hd2020RealtimeHeight
		}
		return mode == hd2020BitmapFullColorRGB && (width > hd2020RealtimeWidth || height > hd2020RealtimeHeight)
	}
	return false
}

func hd2020BoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isHD2020FullColorCard(cardType byte) bool {
	switch cardType {
	case 47, 87, 93: // HD-E63, HD-E63-1, HD-E63-2
		return true
	default:
		return false
	}
}

func colorNeedsHD2020RGB(hex string) bool {
	_, _, b, ok := parseHD2020ColorValue(hex)
	return ok && b > 0
}

type hd2020RenderedLine struct {
	img            *image.Alpha
	baseWidth      int
	baseHeight     int
	renderedWidth  int
	renderedHeight int
}

func renderHD2020TextMask(text string, width, height int, config TextConfig) ([]bool, error) {
	if width <= 0 || height <= 0 || width%8 != 0 {
		return nil, fmt.Errorf("geçersiz HD2020 bitmap boyutu: %dx%d", width, height)
	}

	text = normalizeHD2020Text(text)
	if text == "" {
		text = " "
	}

	lines := splitHD2020TextLines(text)
	face := basicfont.Face7x13
	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	ascent := metrics.Ascent.Ceil()

	margin := maxInt(2, minInt(width, height)/16)
	gap := maxInt(1, height/16)
	if len(lines) <= 1 {
		gap = 0
	}
	maxLineWidth := width - margin*2
	if maxLineWidth < 1 {
		maxLineWidth = width
	}
	availableHeight := height - margin*2 - gap*(len(lines)-1)
	if availableHeight < len(lines) {
		availableHeight = height - gap*(len(lines)-1)
	}
	lineBoxHeight := maxInt(1, availableHeight/len(lines))

	rendered := make([]hd2020RenderedLine, 0, len(lines))
	totalHeight := 0
	for _, line := range lines {
		renderedLine := renderHD2020Line(line, face, ascent, lineHeight, maxLineWidth, lineBoxHeight)
		rendered = append(rendered, renderedLine)
		totalHeight += renderedLine.renderedHeight
	}
	totalHeight += gap * (len(rendered) - 1)

	y := alignHD2020Vertical(config.VAlign, height, totalHeight, margin)
	mask := make([]bool, width*height)
	for _, line := range rendered {
		x := alignHD2020Horizontal(config.HAlign, width, line.renderedWidth, margin)
		paintHD2020ScaledLine(mask, width, height, line, x, y)
		y += line.renderedHeight + gap
	}

	return mask, nil
}

func renderHD2020Line(line string, face font.Face, ascent, lineHeight, maxWidth, maxHeight int) hd2020RenderedLine {
	drawer := &font.Drawer{Face: face}
	baseWidth := drawer.MeasureString(line).Ceil()
	if baseWidth < 1 {
		baseWidth = 1
	}
	if maxHeight < 1 {
		maxHeight = lineHeight
	}

	img := image.NewAlpha(image.Rect(0, 0, baseWidth, lineHeight))
	drawer = &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Alpha{A: 255}),
		Face: face,
		Dot:  fixed.P(0, ascent),
	}
	drawer.DrawString(line)

	scaleX := float64(maxWidth) / float64(baseWidth)
	scaleY := float64(maxHeight) / float64(lineHeight)
	scale := math.Min(scaleX, scaleY)
	if scale <= 0 {
		scale = 1
	}

	renderedWidth := maxInt(1, int(math.Round(float64(baseWidth)*scale)))
	renderedHeight := maxInt(1, int(math.Round(float64(lineHeight)*scale)))
	if renderedWidth > maxWidth {
		renderedWidth = maxWidth
	}
	if renderedHeight > maxHeight {
		renderedHeight = maxHeight
	}

	return hd2020RenderedLine{
		img:            img,
		baseWidth:      baseWidth,
		baseHeight:     lineHeight,
		renderedWidth:  renderedWidth,
		renderedHeight: renderedHeight,
	}
}

func paintHD2020ScaledLine(mask []bool, width, height int, line hd2020RenderedLine, x, y int) {
	for dy := 0; dy < line.renderedHeight; dy++ {
		py := y + dy
		if py < 0 || py >= height {
			continue
		}
		sy := int(float64(dy) * float64(line.baseHeight) / float64(line.renderedHeight))
		if sy >= line.baseHeight {
			sy = line.baseHeight - 1
		}
		for dx := 0; dx < line.renderedWidth; dx++ {
			px := x + dx
			if px < 0 || px >= width {
				continue
			}
			sx := int(float64(dx) * float64(line.baseWidth) / float64(line.renderedWidth))
			if sx >= line.baseWidth {
				sx = line.baseWidth - 1
			}
			if line.img.AlphaAt(sx, sy).A != 0 {
				mask[py*width+px] = true
			}
		}
	}
}

func normalizeHD2020Text(text string) string {
	replacer := strings.NewReplacer(
		"ç", "c", "Ç", "C",
		"ğ", "g", "Ğ", "G",
		"ı", "i", "İ", "I",
		"ö", "o", "Ö", "O",
		"ş", "s", "Ş", "S",
		"ü", "u", "Ü", "U",
	)
	return strings.TrimSpace(strings.ToUpper(replacer.Replace(text)))
}

func splitHD2020TextLines(text string) []string {
	words := strings.Fields(text)
	if len(words) <= 1 {
		return []string{text}
	}

	bestSplit := 1
	bestScore := int(^uint(0) >> 1)
	for i := 1; i < len(words); i++ {
		left := strings.Join(words[:i], " ")
		right := strings.Join(words[i:], " ")
		score := absInt(len(left)-len(right)) + maxInt(len(left), len(right))*2
		if score < bestScore {
			bestScore = score
			bestSplit = i
		}
	}

	return []string{
		strings.Join(words[:bestSplit], " "),
		strings.Join(words[bestSplit:], " "),
	}
}

func chooseHD2020TextScale(lines []string, width, height int, face font.Face, lineHeight int) int {
	for scale := 4; scale >= 1; scale-- {
		smallWidth := width / scale
		smallHeight := height / scale
		if smallWidth <= 0 || smallHeight <= 0 {
			continue
		}
		if lineHeight*len(lines) > smallHeight {
			continue
		}

		ok := true
		drawer := &font.Drawer{Face: face}
		for _, line := range lines {
			if drawer.MeasureString(line).Ceil() > smallWidth {
				ok = false
				break
			}
		}
		if ok {
			return scale
		}
	}
	return 1
}

func encodeHD2020TextBitmap(mask []bool, width, height int, hexColor, backgroundHex string, mode hd2020BitmapMode) ([]byte, int, error) {
	if width <= 0 || height <= 0 || width%8 != 0 {
		return nil, 0, fmt.Errorf("geçersiz HD2020 bitmap boyutu: %dx%d", width, height)
	}
	if len(mask) != width*height {
		return nil, 0, fmt.Errorf("geçersiz HD2020 mask boyutu: %d", len(mask))
	}

	rowBytes := width / 8
	switch mode {
	case hd2020BitmapFullColorRGB:
		r, g, b := parseHD2020Color(hexColor)
		bgR, bgG, bgB, hasBackground := parseHD2020ColorValue(backgroundHex)
		bitmap := make([]byte, rowBytes*height*3)
		if hasBackground && (bgR != 0 || bgG != 0 || bgB != 0) {
			for py := 0; py < height; py++ {
				rowBase := py * rowBytes * 3
				if bgR > 0 {
					fillHD2020PlaneRow(bitmap[rowBase : rowBase+rowBytes])
				}
				if bgG > 0 {
					fillHD2020PlaneRow(bitmap[rowBase+rowBytes : rowBase+rowBytes*2])
				}
				if bgB > 0 {
					fillHD2020PlaneRow(bitmap[rowBase+rowBytes*2 : rowBase+rowBytes*3])
				}
			}
		}
		for py := 0; py < height; py++ {
			rowBase := py * rowBytes * 3
			for px := 0; px < width; px++ {
				if !mask[py*width+px] {
					continue
				}
				bit := byte(0x80 >> uint(px%8))
				col := px / 8
				if r > 0 {
					bitmap[rowBase+col] |= bit
				} else {
					bitmap[rowBase+col] &^= bit
				}
				if g > 0 {
					bitmap[rowBase+rowBytes+col] |= bit
				} else {
					bitmap[rowBase+rowBytes+col] &^= bit
				}
				if b > 0 {
					bitmap[rowBase+2*rowBytes+col] |= bit
				} else {
					bitmap[rowBase+2*rowBytes+col] &^= bit
				}
			}
		}
		return bitmap, 3, nil
	default:
		// Older captures use two interleaved rows for each logical row. Keep
		// this layout for small/legacy HD2020 controllers.
		bitmap := make([]byte, rowBytes*height*2)
		for py := 0; py < height; py++ {
			for px := 0; px < width; px++ {
				if !mask[py*width+px] {
					continue
				}
				for dup := 0; dup < 2; dup++ {
					row := py*2 + dup
					idx := row*rowBytes + px/8
					bitmap[idx] |= 0x80 >> uint(px%8)
				}
			}
		}
		return bitmap, 2, nil
	}
}

func fillHD2020PlaneRow(row []byte) {
	for i := range row {
		row[i] = 0xff
	}
}

func parseHD2020Color(hex string) (byte, byte, byte) {
	r, g, b, ok := parseHD2020ColorValue(hex)
	if !ok {
		return 255, 255, 255
	}
	return r, g, b
}

func parseHD2020ColorValue(hex string) (byte, byte, byte, bool) {
	hex = strings.TrimSpace(hex)
	if strings.HasPrefix(hex, "#") {
		hex = strings.TrimPrefix(hex, "#")
	}
	if len(hex) != 6 {
		return 0, 0, 0, false
	}

	parse := func(s string) (byte, bool) {
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0, false
		}
		return byte(v), true
	}
	r, ok := parse(hex[0:2])
	if !ok {
		return 0, 0, 0, false
	}
	g, ok := parse(hex[2:4])
	if !ok {
		return 0, 0, 0, false
	}
	b, ok := parse(hex[4:6])
	if !ok {
		return 0, 0, 0, false
	}
	return r, g, b, true
}

func renderHD2020TextBitmap(text string, width, height int) ([]byte, error) {
	mask, err := renderHD2020TextMask(text, width, height, TextConfig{})
	if err != nil {
		return nil, err
	}
	bitmap, _, err := encodeHD2020TextBitmap(mask, width, height, ColorWhite, "", hd2020BitmapLegacyTwoPlane)
	return bitmap, err
}

func encodeHD2020ProgramBitmap(mask []bool, width, height int, textHex, backgroundHex string) ([]byte, error) {
	if width <= 0 || height <= 0 || width%8 != 0 {
		return nil, fmt.Errorf("geçersiz HD2020 bitmap boyutu: %dx%d", width, height)
	}
	if len(mask) != width*height {
		return nil, fmt.Errorf("geçersiz HD2020 mask boyutu: %d", len(mask))
	}

	rowBytes := width / 8
	textRed, textGreen := hd2020ProgramColorPlanes(textHex, true)
	bgRed, bgGreen := hd2020ProgramColorPlanes(backgroundHex, false)
	bitmap := make([]byte, rowBytes*height*2)

	for py := 0; py < height; py++ {
		rowBase := py * rowBytes * 2
		redPlane := bitmap[rowBase : rowBase+rowBytes]
		greenPlane := bitmap[rowBase+rowBytes : rowBase+rowBytes*2]
		if bgRed {
			fillHD2020PlaneRow(redPlane)
		}
		if bgGreen {
			fillHD2020PlaneRow(greenPlane)
		}
		for px := 0; px < width; px++ {
			if !mask[py*width+px] {
				continue
			}
			bit := byte(0x80 >> uint(px%8))
			col := px / 8
			if textRed {
				redPlane[col] |= bit
			} else {
				redPlane[col] &^= bit
			}
			if textGreen {
				greenPlane[col] |= bit
			} else {
				greenPlane[col] &^= bit
			}
		}
	}

	return bitmap, nil
}

func hd2020ProgramColorPlanes(hex string, defaultWhite bool) (red, green bool) {
	r, g, b, ok := parseHD2020ColorValue(hex)
	if !ok {
		return defaultWhite, defaultWhite
	}
	if r < 32 && g < 32 && b < 32 {
		return false, false
	}
	if r > 180 && g > 180 && b > 180 {
		return true, true
	}
	if r > 140 && g > 90 && b < 120 {
		return true, true
	}
	if g >= r && g >= b {
		return false, true
	}
	if r >= g && r >= b {
		return true, false
	}
	return true, true
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func alignHD2020Horizontal(align HAlign, width, contentWidth, margin int) int {
	switch align {
	case HAlignLeft:
		return margin
	case HAlignRight:
		return width - contentWidth - margin
	default:
		return (width - contentWidth) / 2
	}
}

func alignHD2020Vertical(align VAlign, height, contentHeight, margin int) int {
	switch align {
	case VAlignTop:
		return margin
	case VAlignBottom:
		return height - contentHeight - margin
	default:
		return (height - contentHeight) / 2
	}
}

func buildHD2020RealtimeAreaPackets(bitmap []byte, width, height, planes int, transferID uint16) [][]byte {
	areaData := append(hd2020AreaHeader(width, height, planes), bitmap...)
	chunks := splitHD2020AreaData(areaData, hd2020AreaDataChunkSize(len(areaData)))

	startPayload := make([]byte, 47)
	startPayload[0] = 0x2f
	binary.LittleEndian.PutUint16(startPayload[1:3], transferID)
	startPayload[4] = byte(len(chunks))
	startPayload[6] = 0x01

	packets := [][]byte{
		buildHD2020Packet(0x18, 1, startPayload),
	}
	for i, chunk := range chunks {
		dataPayload := make([]byte, 5+len(chunk))
		dataPayload[0] = 0x05
		binary.LittleEndian.PutUint16(dataPayload[1:3], transferID)
		binary.BigEndian.PutUint16(dataPayload[3:5], uint16(i))
		copy(dataPayload[5:], chunk)
		packets = append(packets, buildHD2020Packet(0x19, byte(i+2), dataPayload))
	}

	endPayload := make([]byte, 3)
	endPayload[0] = 0x03
	binary.LittleEndian.PutUint16(endPayload[1:3], transferID)
	packets = append(packets, buildHD2020Packet(0x1a, byte(len(chunks)+2), endPayload))

	return packets
}

func hd2020AreaDataChunkSize(dataLen int) int {
	if dataLen <= 1024 {
		return 1024
	}
	return 512
}

func buildHD2020ProgramScreenPackets(bitmap []byte, width, height int, transferID uint16) [][]byte {
	screenData := buildHD2020ProgramData(bitmap, width, height)
	chunks := splitHD2020AreaData(screenData, 1000)

	startPayload := make([]byte, 5)
	startPayload[0] = 0x05
	binary.LittleEndian.PutUint16(startPayload[1:3], transferID)
	binary.BigEndian.PutUint16(startPayload[3:5], uint16(len(chunks)))

	packets := [][]byte{
		buildHD2020Packet(0x13, 1, hd2020ProgramScreenPayload(width, height)),
		buildHD2020Packet(0x02, 2, startPayload),
	}
	for i, chunk := range chunks {
		dataPayload := make([]byte, 5+len(chunk))
		dataPayload[0] = 0x05
		binary.LittleEndian.PutUint16(dataPayload[1:3], transferID)
		binary.BigEndian.PutUint16(dataPayload[3:5], uint16(i))
		copy(dataPayload[5:], chunk)
		packets = append(packets, buildHD2020Packet(0x03, byte(i+3), dataPayload))
	}

	endPayload := make([]byte, 3)
	endPayload[0] = 0x03
	binary.LittleEndian.PutUint16(endPayload[1:3], transferID)
	packets = append(packets, buildHD2020Packet(0x04, byte(len(chunks)+3), endPayload))

	return packets
}

func buildHD2020ProgramData(bitmap []byte, width, height int) []byte {
	chunkCount := (144 + len(bitmap) + 999) / 1000
	hc := []byte{
		0x48, 0x43, 0x19, 0x00, 0x01, 0xff, 0xff, 0xff,
		0xff, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x1d,
	}
	binary.BigEndian.PutUint16(hc[9:11], uint16(chunkCount))

	hp := []byte{
		0x48, 0x50, 0x00, 0x33, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x51, 0x7f, 0x00, 0x00, 0x02, 0x58,
		0x7f, 0x00, 0x6a, 0x40, 0x64, 0x00, 0x6a, 0x4a,
		0xef, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x08, 0x73, 0x2d, 0xff, 0x18, 0xfb, 0xdb, 0x0a,
		0x8a, 0x7a, 0x5c, 0x1a, 0x01, 0x52, 0x73, 0xd1,
		0x00, 0x01, 0xff, 0x00, 0x00, 0x00, 0x00, 0x3c,
		0xff, 0xff, 0xff, 0xff,
	}

	ha := []byte{
		0x48, 0x41, 0x00, 0x19, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x80, 0x00, 0x40, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x1d, 0x00, 0x13, 0x30,
		0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0xff, 0xff,
		0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x1a,
		0x00, 0xc9, 0x00, 0x1e, 0x03, 0x00, 0x00,
	}
	binary.BigEndian.PutUint16(ha[8:10], uint16(width))
	binary.BigEndian.PutUint16(ha[10:12], uint16(height))

	data := make([]byte, 0, len(hc)+len(hp)+len(ha)+len(bitmap))
	data = append(data, hc...)
	data = append(data, hp...)
	data = append(data, ha...)
	data = append(data, bitmap...)
	return data
}

func buildHD2020RealtimeScreenSetupPackets(width, height int, transferID uint16) [][]byte {
	screenPayload := hd2020RealtimeScreenPayload(width, height)
	areaData := hd2020RealtimeAreaDefinition(width, height)

	startPayload := make([]byte, 47)
	startPayload[0] = 0x2f
	binary.LittleEndian.PutUint16(startPayload[1:3], transferID)
	startPayload[4] = 0x01
	startPayload[6] = 0x06
	startPayload[14] = 0x3a

	dataPayload := make([]byte, 5+len(areaData))
	dataPayload[0] = 0x05
	binary.LittleEndian.PutUint16(dataPayload[1:3], transferID)
	copy(dataPayload[5:], areaData)

	endPayload := make([]byte, 3)
	endPayload[0] = 0x03
	binary.LittleEndian.PutUint16(endPayload[1:3], transferID)

	return [][]byte{
		buildHD2020Packet(0x12, 1, []byte{0x02, 0x00}),
		buildHD2020Packet(0x13, 2, screenPayload),
		buildHD2020Packet(0x18, 3, startPayload),
		buildHD2020Packet(0x19, 4, dataPayload),
		buildHD2020Packet(0x1a, 5, endPayload),
	}
}

func hd2020ProgramScreenPayload(width, height int) []byte {
	payload := []byte{
		0x48, 0x53, 0x00, 0x42,
		0x00, 0x80, 0x00, 0x40,
		0x00, 0x01, 0x02, 0x06,
		0x00, 0x54, 0x60, 0x01,
		0x35, 0x60, 0x00, 0x00,
		0x00, 0x31, 0x31, 0x31,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x04, 0x00, 0x00, 0x00,
		0x08, 0x00, 0x08, 0x00,
		0x00, 0x00, 0x08, 0x00,
		0x10, 0x00, 0x08, 0x00,
		0x04, 0x00, 0x10, 0x00,
		0x08, 0x00, 0x08, 0x01,
		0x01, 0x00, 0x00, 0x00,
		0x00, 0x00,
	}
	binary.BigEndian.PutUint16(payload[4:6], uint16(width))
	binary.BigEndian.PutUint16(payload[6:8], uint16(height))
	return payload
}

func hd2020RealtimeScreenPayload(width, height int) []byte {
	payload := []byte{
		0x48, 0x53, 0x00, 0x42,
		0x00, 0x80, 0x00, 0x40,
		0x00, 0x01, 0x02, 0x06,
		0x00, 0x54, 0x60, 0x01,
		0x35, 0x60, 0x00, 0x00,
		0x00, 0x31, 0x31, 0x31,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x04, 0x00, 0x00, 0x00,
		0x08, 0x00, 0x08, 0x00,
		0x00, 0x00, 0x08, 0x00,
		0x10, 0x00, 0x08, 0x00,
		0x04, 0x00, 0x10, 0x00,
		0x08, 0x00, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	}
	binary.BigEndian.PutUint16(payload[4:6], uint16(width))
	binary.BigEndian.PutUint16(payload[6:8], uint16(height))
	return payload
}

func hd2020RealtimeAreaDefinition(width, height int) []byte {
	payload := []byte{
		0x48, 0x4c, 0x00, 0x06,
		0x00, 0x01, 0x00, 0x00,
		0x00, 0x0a,
		0x48, 0x41, 0x00, 0x1d,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x80, 0x00, 0x40,
		0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x21, 0x00, 0x0f,
		0x39, 0x00, 0x00, 0x00,
		0x15, 0x00, 0x00, 0x00,
		0x01, 0xff, 0xff, 0xff,
		0xff,
	}
	binary.BigEndian.PutUint16(payload[18:20], uint16(width))
	binary.BigEndian.PutUint16(payload[20:22], uint16(height))
	return payload
}

func splitHD2020AreaData(data []byte, chunkSize int) [][]byte {
	if chunkSize <= 0 || len(data) <= chunkSize {
		return [][]byte{data}
	}

	chunks := make([][]byte, 0, (len(data)+chunkSize-1)/chunkSize)
	for len(data) > 0 {
		n := chunkSize
		if len(data) < n {
			n = len(data)
		}
		chunks = append(chunks, data[:n])
		data = data[n:]
	}
	return chunks
}

func hd2020AreaHeader(width, height, planes int) []byte {
	header := []byte{
		0x48, 0x41, 0x00, 0x19,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x40, 0x00, 0x20,
		0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x1d, 0x00, 0x17, 0x39,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x1e,
		0xc9, 0x03, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x17,
		0x00, 0x00, 0x02, 0x00,
	}
	binary.BigEndian.PutUint16(header[8:10], uint16(width))
	binary.BigEndian.PutUint16(header[10:12], uint16(height))
	header[50] = byte(planes)
	return header
}

func buildHD2020Packet(cmd byte, sequence byte, payload []byte) []byte {
	const headerLen = 27
	totalLen := headerLen + len(payload) + 3
	packet := make([]byte, totalLen)
	packet[0] = 'H'
	packet[1] = 'T'
	packet[3] = 0x1b
	binary.BigEndian.PutUint16(packet[4:6], uint16(totalLen))
	packet[6] = cmd
	packet[26] = sequence
	copy(packet[headerLen:], payload)

	sum := 0
	for _, b := range packet[:totalLen-3] {
		sum += int(b)
	}
	packet[totalLen-3] = byte(sum >> 8)
	packet[totalLen-2] = byte(sum)
	packet[totalLen-1] = 0xaa
	return packet
}

func (d *Device) sendHD2020Packets(packets [][]byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	addr := fmt.Sprintf("%s:%d", d.host, d.port)
	conn, err := net.DialTimeout("tcp", addr, d.opts.timeout)
	if err != nil {
		return fmt.Errorf("HD2020/Gen6 TCP bağlantı hatası: %w", err)
	}
	defer conn.Close()

	for _, packet := range packets {
		cmd := packet[6]
		conn.SetWriteDeadline(time.Now().Add(d.opts.timeout))
		if _, err := conn.Write(packet); err != nil {
			return fmt.Errorf("HD2020/Gen6 paket gönderilemedi (cmd=0x%02x): %w", cmd, err)
		}
		if err := readHD2020Ack(conn, cmd, d.opts.timeout); err != nil {
			return err
		}
	}
	return nil
}

func readHD2020Ack(conn net.Conn, expectedCmd byte, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn.SetReadDeadline(deadline)
		buf := make([]byte, 31)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return fmt.Errorf("HD2020/Gen6 yanıtı okunamadı: %w", err)
		}
		if len(buf) < 22 || buf[0] != 'H' || buf[1] != 'R' {
			return fmt.Errorf("HD2020/Gen6 beklenmeyen yanıt")
		}
		if buf[20] == expectedCmd {
			if buf[21] != 0x00 {
				return fmt.Errorf("HD2020/Gen6 komut hatası (cmd=0x%02x, status=0x%02x)", expectedCmd, buf[21])
			}
			return nil
		}
		if buf[21] != 0x00 {
			return fmt.Errorf("HD2020/Gen6 beklenmeyen yanıt komutu: got 0x%02x status=0x%02x want 0x%02x", buf[20], buf[21], expectedCmd)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("HD2020/Gen6 beklenen yanıt gelmedi: want 0x%02x", expectedCmd)
		}
	}
}

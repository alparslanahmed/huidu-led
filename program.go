package huidu

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ─── Screen (Ekran) ─────────────────────────────────────────────────────────────
//
// Screen, LED ekranın içerik yapısının en üst seviyesidir.
// Bir Screen, birden fazla Program içerebilir ve her Program sırayla oynatılır.
//
// Hiyerarşi: Screen → Program → Area → Item (Text/Image/Video/Clock)

// Screen, LED ekranın tüm içerik yapısını temsil eder.
type Screen struct {
	// Programs, ekranda oynatılacak programların listesidir.
	// Programlar sırayla oynatılır.
	Programs []*Program

	// isNew, yeni bir ekran oluşturulup oluşturulmadığını belirtir.
	// true ise timeStamps attribute'u eklenir.
	isNew bool
}

// NewScreen, yeni bir Screen oluşturur.
// Bu ekran yapılandırması cihaza SendScreen ile gönderilir.
//
//	screen := huidu.NewScreen()
//	program := screen.AddProgram("Ana Program")
//	area := program.AddArea(0, 0, 64, 32)
//	area.AddText("Merhaba Dünya!", huidu.TextConfig{
//	    FontSize: 12,
//	    Color:    "#ff0000",
//	})
//	err := dev.SendScreen(screen)
func NewScreen() *Screen {
	return &Screen{
		isNew: true,
	}
}

// AddProgram, ekrana yeni bir program ekler ve döner.
// name parametresi programın görünen adıdır.
//
//	program := screen.AddProgram("Program 1")
func (s *Screen) AddProgram(name string) *Program {
	p := &Program{
		Type: ProgramNormal,
		ID:   len(s.Programs),
		GUID: uuid.New().String(),
		Name: name,
	}
	s.Programs = append(s.Programs, p)
	return p
}

// AddProgramWithConfig, gelişmiş yapılandırma ile program ekler.
//
//	program := screen.AddProgramWithConfig(huidu.ProgramConfig{
//	    Name: "Acil Durum",
//	    Type: huidu.ProgramNormal,
//	    PlayCount: 3,
//	    Realtime: true,
//	})
func (s *Screen) AddProgramWithConfig(config ProgramConfig) *Program {
	p := &Program{
		Type:      config.Type,
		ID:        len(s.Programs),
		GUID:      uuid.New().String(),
		Name:      config.Name,
		Realtime:  config.Realtime,
		PlayCount: config.PlayCount,
		Duration:  config.Duration,
		Disabled:  config.Disabled,
	}
	s.Programs = append(s.Programs, p)
	return p
}

// toXML, Screen'i SDK XML formatına dönüştürür.
func (s *Screen) toXML() string {
	var screenAttrs []string
	if s.isNew {
		ts := time.Now().UnixMilli()
		screenAttrs = append(screenAttrs, "timeStamps", fmt.Sprintf("%d", ts))
	}

	var children []string
	for _, p := range s.Programs {
		children = append(children, p.toXML())
	}

	return xmlElementWithChildren("screen", screenAttrs, children...)
}

// ─── Program (Program) ──────────────────────────────────────────────────────────

// ProgramConfig, program oluşturma için yapılandırma parametreleridir.
type ProgramConfig struct {
	// Name, programın görünen adıdır.
	Name string

	// Type, program tipidir (varsayılan: ProgramNormal).
	Type ProgramType

	// Realtime, gerçek zamanlı program bayrağıdır.
	// true ise program hemen oynatılmaya başlar.
	Realtime bool

	// PlayCount, programın kaç kez oynatılacağıdır (0=süresiz, 1-999).
	// 0 ise Duration kullanılır.
	PlayCount int

	// Duration, programın toplam oynatma süresidir.
	// PlayCount 0 olduğunda geçerlidir. Format: "hh:mm:ss"
	Duration string

	// Disabled, programın devre dışı bırakılıp bırakılmadığını belirtir.
	Disabled bool
}

// Program, LED ekranda oynatılacak bir programı temsil eder.
// Her program birden fazla Area (alan) içerebilir.
type Program struct {
	// Type, program tipidir.
	Type ProgramType

	// ID, programın sıra numarasıdır.
	ID int

	// GUID, programın benzersiz kimliğidir.
	GUID string

	// Name, programın görünen adıdır.
	Name string

	// Areas, programdaki alanların listesidir.
	Areas []*Area

	// Realtime, gerçek zamanlı program bayrağıdır.
	Realtime bool

	// PlayCount, oynatma sayısıdır.
	PlayCount int

	// Duration, oynatma süresidir.
	Duration string

	// Disabled, devre dışı bayrağıdır.
	Disabled bool
}

// AddArea, programa yeni bir alan ekler.
// x, y: Alanın sol üst köşe koordinatları (piksel)
// width, height: Alanın boyutları (piksel)
//
//	area := program.AddArea(0, 0, 64, 32) // Tam ekran alan
func (p *Program) AddArea(x, y, width, height int) *Area {
	a := &Area{
		GUID:   uuid.New().String(),
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
		Alpha:  255,
	}
	p.Areas = append(p.Areas, a)
	return a
}

// AddFullScreenArea, tam ekran boyutunda bir alan ekler.
// Genellikle ekran boyutları cihaz bilgisinden alınır.
//
//	info := dev.CachedDeviceInfo()
//	area := program.AddFullScreenArea(info.ScreenWidth, info.ScreenHeight)
func (p *Program) AddFullScreenArea(screenWidth, screenHeight int) *Area {
	return p.AddArea(0, 0, screenWidth, screenHeight)
}

// toXML, Program'ı SDK XML formatına dönüştürür.
func (p *Program) toXML() string {
	attrs := []string{
		"type", string(p.Type),
		"id", fmt.Sprintf("%d", p.ID),
		"guid", p.GUID,
		"name", p.Name,
	}

	if p.Realtime {
		attrs = append(attrs, "flag", "realtime")
	}

	var children []string

	// Play control
	playAttrs := []string{}
	if p.PlayCount > 0 {
		playAttrs = append(playAttrs, "count", fmt.Sprintf("%d", p.PlayCount))
	} else if p.Duration != "" {
		playAttrs = append(playAttrs, "duration", p.Duration)
	} else {
		playAttrs = append(playAttrs, "count", "1")
	}
	playAttrs = append(playAttrs, "disabled", boolStr(p.Disabled))
	children = append(children, xmlElementWithChildren("playControl", playAttrs))

	// Areas
	for _, a := range p.Areas {
		children = append(children, a.toXML())
	}

	return xmlElementWithChildren("program", attrs, children...)
}

// ─── Area (Alan) ────────────────────────────────────────────────────────────────

// Area, program içinde belirli bir dikdörtgen bölgeyi temsil eder.
// Her alan birden fazla içerik öğesi (metin, görsel, video, saat) içerebilir.
// Öğeler sırayla gösterilir.
type Area struct {
	// GUID, alanın benzersiz kimliğidir.
	GUID string

	// Name, alanın opsiyonel adıdır.
	Name string

	// X, Y, alanın sol üst köşe koordinatlarıdır (piksel).
	X, Y int

	// Width, Height, alanın boyutlarıdır (piksel).
	Width, Height int

	// Alpha, alanın saydamlık değeridir (0-255, 255=opak).
	Alpha int

	// items, alandaki içerik öğelerinin listesidir.
	items []areaItem
}

// areaItem, alana eklenebilecek içerik öğelerinin ortak arayüzüdür.
type areaItem interface {
	toXML() string
}

// AddText, alana metin öğesi ekler.
// text: Görüntülenecek metin
// config: Metin yapılandırma parametreleri (isteğe bağlı alanlar)
//
//	area.AddText("Merhaba!", huidu.TextConfig{
//	    FontSize: 14,
//	    Color:    "#ff0000",
//	    Bold:     true,
//	    Effect:   huidu.EffectLeftScroll,
//	    Speed:    4,
//	})
func (a *Area) AddText(text string, config TextConfig) {
	if config.FontName == "" {
		config.FontName = "Arial"
	}
	if config.FontSize == 0 {
		config.FontSize = 12
	}
	if config.Color == "" {
		config.Color = "#ff0000"
	}
	if config.HAlign == "" {
		config.HAlign = HAlignCenter
	}
	if config.VAlign == "" {
		config.VAlign = VAlignMiddle
	}

	item := &textItem{
		guid:   uuid.New().String(),
		name:   config.Name,
		text:   text,
		config: config,
	}
	a.items = append(a.items, item)
}

// AddImage, alana görsel öğesi ekler.
// Görsel dosyasının önce UploadFile ile cihaza yüklenmesi gerekir.
//
//	area.AddImage("logo.png", huidu.ImageConfig{
//	    Fit:    huidu.ImageFitStretch,
//	    Effect: huidu.EffectFade,
//	    Duration: 5,
//	})
func (a *Area) AddImage(fileName string, config ImageConfig) {
	if config.Fit == "" {
		config.Fit = ImageFitStretch
	}

	item := &imageItem{
		guid:     uuid.New().String(),
		name:     config.Name,
		fileName: fileName,
		config:   config,
	}
	a.items = append(a.items, item)
}

// AddVideo, alana video öğesi ekler.
// Video dosyasının önce UploadFile ile cihaza yüklenmesi gerekir.
//
//	area.AddVideo("reklam.mp4", huidu.VideoConfig{
//	    AspectRatio: true,
//	})
func (a *Area) AddVideo(fileName string, config VideoConfig) {
	item := &videoItem{
		guid:     uuid.New().String(),
		name:     config.Name,
		fileName: fileName,
		config:   config,
	}
	a.items = append(a.items, item)
}

// AddClock, alana saat öğesi ekler.
//
//	area.AddClock(huidu.ClockConfig{
//	    Type:        huidu.ClockDigital,
//	    ShowDate:    true,
//	    DateFormat:  1,
//	    ShowTime:    true,
//	    TimeFormat:  1,
//	    TimeColor:   "#00ff00",
//	})
func (a *Area) AddClock(config ClockConfig) {
	if config.Type == "" {
		config.Type = ClockDigital
	}

	item := &clockItem{
		guid:   uuid.New().String(),
		name:   config.Name,
		config: config,
	}
	a.items = append(a.items, item)
}

// toXML, Area'yı SDK XML formatına dönüştürür.
func (a *Area) toXML() string {
	attrs := []string{
		"guid", a.GUID,
		"name", a.Name,
		"alpha", fmt.Sprintf("%d", a.Alpha),
	}

	rectXML := xmlElement("rectangle",
		"x", fmt.Sprintf("%d", a.X),
		"y", fmt.Sprintf("%d", a.Y),
		"width", fmt.Sprintf("%d", a.Width),
		"height", fmt.Sprintf("%d", a.Height),
	)

	var resourceChildren []string
	for _, item := range a.items {
		resourceChildren = append(resourceChildren, item.toXML())
	}
	resourcesXML := xmlElementWithChildren("resources", nil, resourceChildren...)

	return xmlElementWithChildren("area", attrs, rectXML, resourcesXML)
}

// ─── Yapılandırma Tipleri ───────────────────────────────────────────────────────

// TextConfig, metin öğesinin yapılandırma parametreleridir.
type TextConfig struct {
	// Name, öğenin opsiyonel adıdır.
	Name string

	// FontName, font adıdır (varsayılan: "Arial").
	FontName string

	// FontSize, font boyutudur (varsayılan: 12).
	FontSize int

	// Color, metin rengidir (#RRGGBB formatında, varsayılan: "#ff0000").
	Color string

	// Bold, kalın yazı bayrağıdır.
	Bold bool

	// Italic, italik yazı bayrağıdır.
	Italic bool

	// Underline, altı çizili yazı bayrağıdır.
	Underline bool

	// HAlign, yatay hizalama (varsayılan: center).
	HAlign HAlign

	// VAlign, dikey hizalama (varsayılan: middle).
	VAlign VAlign

	// BackgroundColor, arka plan rengidir (#RRGGBB formatında).
	// Boş bırakılırsa arka plan rengi kullanılmaz.
	BackgroundColor string

	// Effect, giriş efekti tipidir (varsayılan: EffectImmediate).
	Effect EffectType

	// OutEffect, çıkış efekti tipidir.
	OutEffect EffectType

	// Speed, efekt hızıdır (1-10, varsayılan: 4).
	Speed int

	// Duration, gösterim süresidir (saniye cinsinden, varsayılan: 3).
	Duration int
}

// ImageConfig, görsel öğesinin yapılandırma parametreleridir.
type ImageConfig struct {
	// Name, öğenin opsiyonel adıdır.
	Name string

	// Fit, görselin alana nasıl yerleştirileceğidir (varsayılan: stretch).
	Fit ImageFit

	// Effect, giriş efekti tipidir.
	Effect EffectType

	// OutEffect, çıkış efekti tipidir.
	OutEffect EffectType

	// Speed, efekt hızıdır (1-10, varsayılan: 4).
	Speed int

	// Duration, gösterim süresidir (saniye, varsayılan: 3).
	Duration int
}

// VideoConfig, video öğesinin yapılandırma parametreleridir.
type VideoConfig struct {
	// Name, öğenin opsiyonel adıdır.
	Name string

	// AspectRatio, en-boy oranının korunup korunmayacağını belirtir.
	AspectRatio bool
}

// ClockConfig, saat öğesinin yapılandırma parametreleridir.
type ClockConfig struct {
	// Name, öğenin opsiyonel adıdır.
	Name string

	// Type, saat tipidir (digital veya dial). Varsayılan: digital.
	Type ClockType

	// Timezone, saat dilimi (ör: "+8:00"). Boş ise cihaz saati kullanılır.
	Timezone string

	// Adjust, zaman ince ayarı (ör: "+00:05:00", "-00:05:00").
	Adjust string

	// ShowTitle, başlık gösterimi.
	ShowTitle bool

	// TitleValue, başlık metni.
	TitleValue string

	// TitleColor, başlık rengi (#RRGGBB).
	TitleColor string

	// ShowDate, tarih gösterimi.
	ShowDate bool

	// DateFormat, tarih formatı (1-7):
	// 1: YYYY/MM/DD, 2: MM/DD/YYYY, 3: DD/MM/YYYY,
	// 4: Jan DD YYYY, 5: DD Jan YYYY, 6: YYYY年MM月DD日, 7: MM月DD日
	DateFormat int

	// DateColor, tarih rengi (#RRGGBB).
	DateColor string

	// ShowWeek, haftanın günü gösterimi.
	ShowWeek bool

	// WeekFormat, gün formatı (1: yerel dil, 2: Monday, 3: Mon).
	WeekFormat int

	// WeekColor, gün rengi (#RRGGBB).
	WeekColor string

	// ShowTime, saat gösterimi.
	ShowTime bool

	// TimeFormat, saat formatı (1: hh:mm:ss, 2: hh:ss, 3: hh時mm分ss秒, 4: hh時mm分).
	TimeFormat int

	// TimeColor, saat rengi (#RRGGBB).
	TimeColor string

	// ShowLunarCalendar, ay takvimi (çin takvimi) gösterimi.
	ShowLunarCalendar bool

	// LunarCalendarColor, ay takvimi rengi (#RRGGBB).
	LunarCalendarColor string
}

// ─── İçerik Öğesi Uygulamaları ──────────────────────────────────────────────────

// textItem, metin içerik öğesidir.
type textItem struct {
	guid   string
	name   string
	text   string
	config TextConfig
}

func (t *textItem) toXML() string {
	c := t.config

	// Sürekli yatay kaydırma efektlerinde singleLine=true olmalı
	singleLine := false
	if c.Effect.IsContinuousScroll() {
		singleLine = true
	}

	attrs := []string{
		"guid", t.guid,
		"name", t.name,
		"singleLine", boolStr(singleLine),
	}
	if c.BackgroundColor != "" {
		attrs = append(attrs, "background", c.BackgroundColor)
	}

	styleXML := xmlElement("style",
		"align", string(c.HAlign),
		"valign", string(c.VAlign),
	)

	stringXML := xmlElementWithContent("string", t.text)

	fontXML := xmlElement("font",
		"name", c.FontName,
		"size", fmt.Sprintf("%d", c.FontSize),
		"color", c.Color,
		"bold", boolStr(c.Bold),
		"italic", boolStr(c.Italic),
		"underline", boolStr(c.Underline),
	)

	effectXML := buildEffectXML(c.Effect, c.OutEffect, c.Speed, c.Duration)

	return xmlElementWithChildren("text", attrs, styleXML, stringXML, fontXML, effectXML)
}

// imageItem, görsel içerik öğesidir.
type imageItem struct {
	guid     string
	name     string
	fileName string
	config   ImageConfig
}

func (i *imageItem) toXML() string {
	c := i.config

	attrs := []string{
		"guid", i.guid,
		"name", i.name,
		"fit", string(c.Fit),
	}

	effectXML := buildEffectXML(c.Effect, c.OutEffect, c.Speed, c.Duration)
	fileXML := xmlElement("file", "name", i.fileName)

	return xmlElementWithChildren("image", attrs, effectXML, fileXML)
}

// videoItem, video içerik öğesidir.
type videoItem struct {
	guid     string
	name     string
	fileName string
	config   VideoConfig
}

func (v *videoItem) toXML() string {
	c := v.config

	attrs := []string{
		"guid", v.guid,
		"name", v.name,
		"aspectRatio", boolStr(c.AspectRatio),
	}

	fileXML := xmlElement("file", "name", v.fileName)
	return xmlElementWithChildren("video", attrs, fileXML)
}

// clockItem, saat içerik öğesidir.
type clockItem struct {
	guid   string
	name   string
	config ClockConfig
}

func (cl *clockItem) toXML() string {
	c := cl.config

	attrs := []string{
		"guid", cl.guid,
		"name", cl.name,
		"type", string(c.Type),
	}
	if c.Timezone != "" {
		attrs = append(attrs, "timezone", c.Timezone)
	}
	adjust := c.Adjust
	if adjust == "" {
		adjust = "00:00:00"
	}
	attrs = append(attrs, "adjust", adjust)

	titleColor := c.TitleColor
	if titleColor == "" {
		titleColor = "#ff0000"
	}
	dateColor := c.DateColor
	if dateColor == "" {
		dateColor = "#ff0000"
	}
	weekColor := c.WeekColor
	if weekColor == "" {
		weekColor = "#ff0000"
	}
	timeColor := c.TimeColor
	if timeColor == "" {
		timeColor = "#ff0000"
	}
	lunarColor := c.LunarCalendarColor
	if lunarColor == "" {
		lunarColor = "#ff0000"
	}

	dateFormat := c.DateFormat
	if dateFormat == 0 {
		dateFormat = 1
	}
	weekFormat := c.WeekFormat
	if weekFormat == 0 {
		weekFormat = 1
	}
	timeFormat := c.TimeFormat
	if timeFormat == 0 {
		timeFormat = 1
	}

	children := []string{
		xmlElement("title",
			"value", c.TitleValue,
			"color", titleColor,
			"display", boolStr(c.ShowTitle),
		),
		xmlElement("date",
			"format", fmt.Sprintf("%d", dateFormat),
			"color", dateColor,
			"display", boolStr(c.ShowDate),
		),
		xmlElement("week",
			"format", fmt.Sprintf("%d", weekFormat),
			"color", weekColor,
			"display", boolStr(c.ShowWeek),
		),
		xmlElement("time",
			"format", fmt.Sprintf("%d", timeFormat),
			"color", timeColor,
			"display", boolStr(c.ShowTime),
		),
		xmlElement("lunarCalendar",
			"color", lunarColor,
			"display", boolStr(c.ShowLunarCalendar),
		),
	}

	return xmlElementWithChildren("clock", attrs, children...)
}

// ─── Efekt Yardımcısı ───────────────────────────────────────────────────────────

// buildEffectXML, efekt XML elementini oluşturur.
func buildEffectXML(inEffect, outEffect EffectType, speed, duration int) string {
	if speed <= 0 {
		speed = 4
	}
	if duration <= 0 {
		duration = 3
	}
	outSpeed := speed

	return xmlElement("effect",
		"in", fmt.Sprintf("%d", int(inEffect)),
		"inSpeed", fmt.Sprintf("%d", speed),
		"out", fmt.Sprintf("%d", int(outEffect)),
		"outSpeed", fmt.Sprintf("%d", outSpeed),
		"duration", fmt.Sprintf("%d", duration*10),
	)
}

// ─── Screen Gönderme ────────────────────────────────────────────────────────────

// SendScreen, ekran yapılandırmasını cihaza gönderir.
// Bu, cihazın mevcut tüm programlarını değiştirir.
//
//	screen := huidu.NewScreen()
//	prog := screen.AddProgram("Demo")
//	area := prog.AddArea(0, 0, 64, 32)
//	area.AddText("Test", huidu.TextConfig{Color: "#ff0000"})
//	err := dev.SendScreen(screen)
func (d *Device) SendScreen(screen *Screen) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return d.sendHD2020Gen6Screen(screen)
	}

	screenXML := screen.toXML()
	fullXML := buildSdkXML(d.sdkGUID, MethodAddProgram, screenXML)

	resp, err := d.sendSdkCmdAndReceive([]byte(fullXML))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SendScreen başarısız: %s", resp.Result)
	}

	return nil
}

// SendText, ekrana tek bir metin göndermek için kısayol fonksiyondur.
// Tam ekran metin alanı oluşturur ve gönderir.
//
// Ekran boyutları cihaz bilgisinden otomatik alınır.
// Bağlantı kurulurken CachedDeviceInfo nil ise varsayılan 64x32 kullanılır.
//
//	err := dev.SendText("Merhaba!", huidu.TextConfig{
//	    FontSize: 14,
//	    Color:    "#00ff00",
//	    Effect:   huidu.EffectLeftScroll,
//	    Speed:    3,
//	})
func (d *Device) SendText(text string, config TextConfig) error {
	w, h := 64, 32
	d.mu.Lock()
	if d.info != nil {
		w = d.info.ScreenWidth
		h = d.info.ScreenHeight
	} else if d.protocol == ProtocolHD2020Gen6 && d.hd2020CardTypeKnown && isHD2020FullColorCard(d.hd2020CardType) {
		w = 128
		h = 64
	}
	d.mu.Unlock()

	screen := NewScreen()
	prog := screen.AddProgram("TextProgram")
	area := prog.AddArea(0, 0, w, h)
	area.AddText(text, config)

	return d.SendScreen(screen)
}

// ─── Program update/delete ──────────────────────────────────────────────────────

// UpdateProgram, belirtilen programı günceller.
// Program'ın GUID'i mevcut bir programla eşleşmelidir.
func (d *Device) UpdateProgram(program *Program) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	programXML := program.toXML()
	fullXML := buildSdkXML(d.sdkGUID, MethodUpdateProgram, programXML)

	resp, err := d.sendSdkCmdAndReceive([]byte(fullXML))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("UpdateProgram başarısız: %s", resp.Result)
	}

	return nil
}

// DeleteProgram, belirtilen programı siler.
// Program'ın GUID'i ile eşleşen program cihazdan kaldırılır.
func (d *Device) DeleteProgram(program *Program) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	programXML := program.toXML()
	fullXML := buildSdkXML(d.sdkGUID, MethodDeleteProgram, programXML)

	resp, err := d.sendSdkCmdAndReceive([]byte(fullXML))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("DeleteProgram başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Renk Yardımcıları ──────────────────────────────────────────────────────────

// ColorRed, kırmızı renk sabiti.
const ColorRed = "#ff0000"

// ColorGreen, yeşil renk sabiti.
const ColorGreen = "#00ff00"

// ColorBlue, mavi renk sabiti.
const ColorBlue = "#0000ff"

// ColorWhite, beyaz renk sabiti.
const ColorWhite = "#ffffff"

// ColorYellow, sarı renk sabiti.
const ColorYellow = "#ffff00"

// ColorCyan, camgöbeği renk sabiti.
const ColorCyan = "#00ffff"

// ColorMagenta, eflatun renk sabiti.
const ColorMagenta = "#ff00ff"

// ColorOrange, turuncu renk sabiti.
const ColorOrange = "#ff8000"

// RGB, 0-255 arası R, G, B değerlerinden #RRGGBB formatında renk string'i oluşturur.
//
//	color := huidu.RGB(255, 128, 0) // "#ff8000"
func RGB(r, g, b int) string {
	return fmt.Sprintf("#%02x%02x%02x", clamp(r), clamp(g), clamp(b))
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// Unused import supressor
var _ = strings.Join

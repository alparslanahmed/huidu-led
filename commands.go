package huidu

import (
	"fmt"
)

// ─── Cihaz Bilgi Komutları ──────────────────────────────────────────────────────

// GetDeviceInfo, cihazın donanım ve yazılım bilgilerini sorgular.
//
// Dönen bilgiler: CPU tipi, model, cihaz ID, firmware/FPGA/kernel versiyonları,
// ekran boyutları ve dönme açısı.
//
//	info, err := dev.GetDeviceInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Model: %s, Ekran: %dx%d\n", info.Model, info.ScreenWidth, info.ScreenHeight)
func (d *Device) GetDeviceInfo() (*DeviceInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		if d.info != nil {
			return d.info, nil
		}
		return nil, fmt.Errorf("%w: HD2020/Gen6 cihaz bilgisi sorgusu bu backend'de desteklenmiyor", ErrUnsupportedProtocol)
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetDeviceInfo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetDeviceInfo başarısız: %s", resp.Result)
	}

	info, err := parseDeviceInfoXML(resp.InnerXML)
	if err != nil {
		return nil, err
	}

	// Önbelleği güncelle
	d.mu.Lock()
	d.info = info
	d.mu.Unlock()

	return info, nil
}

// ─── Ağ Yapılandırma Komutları ──────────────────────────────────────────────────

// GetEthernetInfo, cihazın Ethernet ağ yapılandırmasını sorgular.
//
//	eth, err := dev.GetEthernetInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("IP: %s, DHCP: %v\n", eth.IP, eth.AutoDHCP)
func (d *Device) GetEthernetInfo() (*EthernetInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetEth0Info, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetEthernetInfo başarısız: %s", resp.Result)
	}

	return parseEthernetInfoXML(resp.InnerXML)
}

// SetEthernetInfo, cihazın Ethernet ağ yapılandırmasını ayarlar.
//
// DİKKAT: Bu komut cihazın IP adresini değiştirebilir. Yanlış ayarlar
// cihaza erişimi engelleyebilir. Değişiklik sonrası cihaz yeniden başlatılabilir.
//
//	err := dev.SetEthernetInfo(&huidu.EthernetInfo{
//	    Enabled:  true,
//	    AutoDHCP: false,
//	    IP:       "192.168.6.1",
//	    Netmask:  "255.255.255.0",
//	    Gateway:  "192.168.6.254",
//	    DNS:      "8.8.8.8",
//	})
func (d *Device) SetEthernetInfo(info *EthernetInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	inner := buildSetEthernetXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetEth0Info, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetEthernetInfo başarısız: %s", resp.Result)
	}

	return nil
}

// GetWifiInfo, cihazın WiFi modül bilgilerini sorgular.
// WiFi modülü olmayan cihazlarda HasWifi=false döner.
//
//	wifi, err := dev.GetWifiInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if wifi.HasWifi {
//	    fmt.Printf("WiFi SSID: %s, Mod: %d\n", wifi.APInfo.SSID, wifi.WorkMode)
//	}
func (d *Device) GetWifiInfo() (*WifiInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetWifiInfo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetWifiInfo başarısız: %s", resp.Result)
	}

	return parseWifiInfoXML(resp.InnerXML)
}

// SetWifiInfo, cihazın WiFi ayarlarını yapılandırır.
// WiFi modülü olmayan cihazlarda hata döner.
func (d *Device) SetWifiInfo(info *WifiInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	// WiFi set XML oluştur
	modeStr := "ap"
	if info.WorkMode == 1 {
		modeStr = "station"
	}

	inner := xmlElement("mode", "value", modeStr)
	inner += xmlElementWithChildren("ap", nil,
		xmlElement("ssid", "value", info.APInfo.SSID),
		xmlElement("passwd", "value", info.APInfo.Password),
		xmlElement("channel", "value", info.APInfo.Channel),
		xmlElement("encryption", "value", "WPA-PSK"),
		xmlElement("dhcp", "auto", "true"),
		xmlElement("address", "ip", "0.0.0.0", "netmask", "0.0.0.0", "gateway", "0.0.0.0", "dns", "0.0.0.0"),
	)
	inner += xmlElementWithChildren("station", nil,
		xmlElement("ssid", "value", info.StationSSID),
		xmlElement("passwd", "value", info.StationPass),
	)

	xmlData := buildSdkXML(d.sdkGUID, MethodSetWifiInfo, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetWifiInfo başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Parlaklık Komutları ────────────────────────────────────────────────────────

// GetLuminanceInfo, cihazın parlaklık ayar bilgilerini sorgular.
//
// Parlaklık modları:
//
//   - Mode 0 (default): Sabit parlaklık (DefaultValue)
//
//   - Mode 1 (ploys): Zamanlı parlaklık (CustomItems'daki kurallara göre)
//
//   - Mode 2 (sensor): Işık sensörü (SensorMin/SensorMax aralığında otomatik)
//
//     lum, err := dev.GetLuminanceInfo()
//     if err != nil {
//     log.Fatal(err)
//     }
//     fmt.Printf("Mod: %d, Parlaklık: %d%%\n", lum.Mode, lum.DefaultValue)
func (d *Device) GetLuminanceInfo() (*LuminanceInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return nil, fmt.Errorf("%w: HD2020/Gen6 parlaklık okuma bu backend'de desteklenmiyor", ErrUnsupportedProtocol)
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetLuminancePloy, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetLuminanceInfo başarısız: %s", resp.Result)
	}

	return parseLuminanceInfoXML(resp.InnerXML)
}

// SetLuminanceInfo, cihazın parlaklık ayarlarını yapar.
//
// Basit kullanım (sabit parlaklık):
//
//	err := dev.SetLuminanceInfo(&huidu.LuminanceInfo{
//	    Mode:         0,
//	    DefaultValue: 80, // %80 parlaklık
//	})
//
// Zamanlı parlaklık:
//
//	err := dev.SetLuminanceInfo(&huidu.LuminanceInfo{
//	    Mode:         1,
//	    DefaultValue: 100,
//	    CustomItems: []huidu.LuminanceItem{
//	        {Enabled: true, Start: "06:00:00", Percent: 100},
//	        {Enabled: true, Start: "18:00:00", Percent: 50},
//	        {Enabled: true, Start: "22:00:00", Percent: 20},
//	    },
//	})
func (d *Device) SetLuminanceInfo(info *LuminanceInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return fmt.Errorf("%w: HD2020/Gen6 parlaklık ayarı bu backend'de desteklenmiyor", ErrUnsupportedProtocol)
	}

	// C# SDK varsayılan değerleri: sensorMin=1, sensorMax=100, sensorTime=10
	if info.SensorMin <= 0 {
		info.SensorMin = 1
	}
	if info.SensorMax <= 0 {
		info.SensorMax = 100
	}
	if info.SensorTime <= 0 {
		info.SensorTime = 10
	}

	inner := buildSetLuminanceXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetLuminancePloy, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetLuminanceInfo başarısız: %s", resp.Result)
	}

	return nil
}

// SetBrightness, parlaklığı kolayca ayarlamak için kısayol fonksiyondur.
// value: 1-100 arası parlaklık yüzdesi.
//
//	err := dev.SetBrightness(80) // %80 parlaklık
func (d *Device) SetBrightness(value int) error {
	if value < 1 {
		value = 1
	}
	if value > 100 {
		value = 100
	}
	return d.SetLuminanceInfo(&LuminanceInfo{
		Mode:         0,
		DefaultValue: value,
	})
}

// ─── Zaman Komutları ────────────────────────────────────────────────────────────

// GetTimeInfo, cihazın zaman ayarlarını sorgular.
//
//	ti, err := dev.GetTimeInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Saat Dilimi: %s, Senkronizasyon: %s\n", ti.Timezone, ti.Sync)
func (d *Device) GetTimeInfo() (*TimeInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetTimeInfo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetTimeInfo başarısız: %s", resp.Result)
	}

	return parseTimeInfoXML(resp.InnerXML)
}

// SetTimeInfo, cihazın zaman ayarlarını yapar.
//
//	err := dev.SetTimeInfo(&huidu.TimeInfo{
//	    Timezone: "(UTC+03:00)Istanbul",
//	    Summer:   false,
//	    Sync:     "none",
//	    Time:     "2024-01-15 14:30:00",
//	})
func (d *Device) SetTimeInfo(info *TimeInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	inner := buildSetTimeXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetTimeInfo, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetTimeInfo başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Ekran Açma/Kapama Komutları ────────────────────────────────────────────────

// OpenScreen, LED ekranı hemen açar.
//
//	err := dev.OpenScreen()
func (d *Device) OpenScreen() error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodOpenScreen, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("OpenScreen başarısız: %s", resp.Result)
	}

	return nil
}

// CloseScreen, LED ekranı hemen kapatır (karartır).
// Ekran fiziksel olarak kapatılmaz, sadece LED'ler söndürülür.
//
//	err := dev.CloseScreen()
func (d *Device) CloseScreen() error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodCloseScreen, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("CloseScreen başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Zamanlı Açma/Kapama Komutları ──────────────────────────────────────────────

// GetSwitchTimeInfo, zamanlı açma/kapama kurallarını sorgular.
//
//	sw, err := dev.GetSwitchTimeInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Zamanlı kontrol aktif: %v, Kural sayısı: %d\n", sw.PloyEnabled, len(sw.Items))
func (d *Device) GetSwitchTimeInfo() (*SwitchTimeInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetSwitchTime, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetSwitchTimeInfo başarısız: %s", resp.Result)
	}

	return parseSwitchTimeInfoXML(resp.InnerXML)
}

// SetSwitchTimeInfo, zamanlı açma/kapama kurallarını ayarlar.
//
//	err := dev.SetSwitchTimeInfo(&huidu.SwitchTimeInfo{
//	    OpenEnabled: true,
//	    PloyEnabled: true,
//	    Items: []huidu.SwitchTimeItem{
//	        {Enabled: true, Start: "08:00:00", End: "22:00:00"},
//	    },
//	})
func (d *Device) SetSwitchTimeInfo(info *SwitchTimeInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	inner := buildSetSwitchTimeXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetSwitchTime, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetSwitchTimeInfo başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Açılış Logosu Komutları ────────────────────────────────────────────────────

// GetBootLogoInfo, açılış logosu bilgisini sorgular.
func (d *Device) GetBootLogoInfo() (*BootLogoInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetBootLogo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetBootLogoInfo başarısız: %s", resp.Result)
	}

	return parseBootLogoInfoXML(resp.InnerXML)
}

// SetBootLogo, açılış logosunu ayarlar.
// Önce görsel dosyasını UploadFile ile cihaza yükleyin.
func (d *Device) SetBootLogo(info *BootLogoInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	inner := buildSetBootLogoXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetBootLogoName, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetBootLogo başarısız: %s", resp.Result)
	}

	return nil
}

// ClearBootLogo, açılış logosunu temizler.
func (d *Device) ClearBootLogo() error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodClearBootLogo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("ClearBootLogo başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Font Komutları ─────────────────────────────────────────────────────────────

// GetFontInfo, cihazda yüklü font listesini sorgular.
//
//	fonts, err := dev.GetFontInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, f := range fonts {
//	    fmt.Printf("Font: %s (%s)\n", f.FontName, f.FileName)
//	}
func (d *Device) GetFontInfo() ([]FontInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetAllFontInfo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetFontInfo başarısız: %s", resp.Result)
	}

	return parseFontInfoXML(resp.InnerXML)
}

// ─── Sunucu Bilgi Komutları ─────────────────────────────────────────────────────

// GetServerInfo, cihazın bağlandığı TCP sunucu bilgisini sorgular.
func (d *Device) GetServerInfo() (*ServerInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetSDKTcpServer, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetServerInfo başarısız: %s", resp.Result)
	}

	return parseServerInfoXML(resp.InnerXML)
}

// SetServerInfo, cihazın bağlanacağı TCP sunucu bilgisini ayarlar.
//
// DİKKAT: Bu ayar, cihazın uzak sunucuya bağlanmasını sağlar.
// Yanlış ayar uzak erişimi engelleyebilir.
func (d *Device) SetServerInfo(info *ServerInfo) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	inner := buildSetServerXML(info)
	xmlData := buildSdkXML(d.sdkGUID, MethodSetSDKTcpServer, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("SetServerInfo başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Dosya Yönetimi Komutları ───────────────────────────────────────────────────

// GetFileList, cihaza yüklenmiş dosya listesini sorgular.
//
//	files, err := dev.GetFileList()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, f := range files {
//	    fmt.Printf("%s (%d bytes, MD5: %s)\n", f.Name, f.Size, f.MD5)
//	}
func (d *Device) GetFileList() ([]FileInfo, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	xmlData := buildSdkXML(d.sdkGUID, MethodGetFiles, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("GetFileList başarısız: %s", resp.Result)
	}

	return parseFileListXML(resp.InnerXML)
}

// DeleteFiles, cihazdan belirtilen dosyaları siler.
//
//	err := dev.DeleteFiles("image1.jpg", "video1.mp4")
func (d *Device) DeleteFiles(fileNames ...string) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	if len(fileNames) == 0 {
		return fmt.Errorf("en az bir dosya adı belirtilmeli")
	}

	inner := buildDeleteFilesXML(fileNames)
	xmlData := buildSdkXML(d.sdkGUID, MethodDeleteFiles, inner)
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("DeleteFiles başarısız: %s", resp.Result)
	}

	return nil
}

// ─── Program Yönetimi Komutları ─────────────────────────────────────────────────

// DeleteAllPrograms, cihazdan tüm programları siler.
// Ekran boş kalır.
//
// C# SDK'da AddProgram metodu ekrandaki tüm programları değiştirir (replace).
// Boş bir screen göndermek tüm programları silmek anlamına gelir.
func (d *Device) DeleteAllPrograms() error {
	if err := d.ensureConnected(); err != nil {
		return err
	}

	// Boş bir screen oluştur (program yok) ve AddProgram ile gönder.
	// AddProgram, mevcut tüm programları bu boş ekranla değiştirir → ekran temizlenir.
	emptyScreen := NewScreen()
	screenXML := emptyScreen.toXML()
	xmlData := buildSdkXML(d.sdkGUID, MethodAddProgram, screenXML)

	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("DeleteAllPrograms başarısız: %s", resp.Result)
	}

	return nil
}

// SendRawXML, ham XML komutunu cihaza gönderir ve yanıtı bekler.
// İleri düzey kullanıcılar için düşük seviyeli erişim sağlar.
//
// GUID otomatik olarak mevcut oturum GUID'i ile değiştirilir.
func (d *Device) SendRawXML(xmlStr string) (*SdkResponse, error) {
	if err := d.ensureConnected(); err != nil {
		return nil, err
	}

	// GUID'i güncelle
	currentGUID := extractGUID(xmlStr)
	if currentGUID != "" && currentGUID != d.sdkGUID {
		xmlStr = replaceGUID(xmlStr, currentGUID, d.sdkGUID)
	}

	return d.sendSdkCmdAndReceive([]byte(xmlStr))
}

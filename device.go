package huidu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Device, bir Huidu LED kontrol kartıyla TCP bağlantısını yöneten
// ana yapıdır. Thread-safe olarak tasarlanmıştır.
//
// Kullanım:
//
//	dev := huidu.NewDevice("192.168.6.1", 10001)
//	err := dev.Connect()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer dev.Close()
//
//	info, err := dev.GetDeviceInfo()
type Device struct {
	// host, cihazın IP adresidir.
	host string

	// port, cihazın TCP port numarasıdır.
	port int

	// conn, aktif TCP bağlantısıdır.
	conn net.Conn

	// sdkGUID, bu oturum için benzersiz kimlik.
	// Handshake sırasında cihazdan alınır.
	sdkGUID string

	// opts, cihaz yapılandırma seçenekleridir.
	opts deviceOptions

	// mu, bağlantı durumu için mutex'tir.
	mu sync.Mutex

	// writeMu, TCP yazma işlemleri için mutex'tir.
	// Aynı anda birden fazla goroutine yazmasını engeller.
	writeMu sync.Mutex

	// connected, bağlantı durumunu gösterir.
	connected bool

	// protocol, bağlantı kurulan cihazın tel protokol ailesidir.
	protocol ProtocolKind

	// hd2020CardType, HD2020/Gen6 probe yanıtından okunan kart tipidir.
	hd2020CardType      byte
	hd2020CardTypeKnown bool
	hd2020DeviceID      string

	// stopHeartbeat, heartbeat goroutine'ini durdurmak için kullanılır.
	stopHeartbeat chan struct{}

	// info, cihaz bilgileri (handshake sonrası doldurulur).
	info *DeviceInfo
}

// NewDevice, yeni bir Device nesnesi oluşturur.
// Bağlantı henüz kurulmaz; Connect() çağrılmalıdır.
//
//	// Basit kullanım
//	dev := huidu.NewDevice("192.168.6.1", 10001)
//
//	// Seçeneklerle
//	dev := huidu.NewDevice("192.168.6.1", 10001,
//	    huidu.WithTimeout(5*time.Second),
//	    huidu.WithLogger(log.Default()),
//	)
func NewDevice(host string, port int, options ...DeviceOption) *Device {
	opts := defaultDeviceOptions()
	for _, opt := range options {
		opt(&opts)
	}

	return &Device{
		host:     host,
		port:     port,
		opts:     opts,
		protocol: ProtocolSDK2TCP,
	}
}

// Connect, cihaza TCP bağlantısı kurar ve 3 aşamalı handshake gerçekleştirir.
//
// Handshake aşamaları:
//  1. Transport Protocol Version anlaşması (0x2001 → 0x2002)
//  2. SDK Version anlaşması (GetIFVersion XML → GUID alınır)
//  3. Device Info sorgulanır (GetDeviceInfo XML → cihaz bilgileri alınır)
//
// Eğer bağlantı zaten kuruluysa, önce mevcut bağlantı kapatılır.
func (d *Device) Connect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Mevcut bağlantıyı kapat
	if d.conn != nil {
		d.closeInternal()
	}
	d.protocol = ProtocolSDK2TCP

	d.logf("TCP bağlantısı kuruluyor: %s:%d", d.host, d.port)

	// TCP bağlantısı kur
	addr := fmt.Sprintf("%s:%d", d.host, d.port)
	conn, err := net.DialTimeout("tcp", addr, d.opts.timeout)
	if err != nil {
		return fmt.Errorf("TCP bağlantı hatası: %w", err)
	}
	d.conn = conn
	d.connected = true

	// Aşama 1: Transport Protocol Version anlaşması
	d.logf("Aşama 1: Transport Protocol Version anlaşması")
	if err := d.handshakeVersion(); err != nil {
		if errors.Is(err, ErrUnsupportedProtocol) {
			if d.conn != nil {
				_ = d.conn.Close()
				d.conn = nil
			}
			d.protocol = ProtocolHD2020Gen6
			d.populateHD2020InfoLocked()
			d.connected = true
			if d.hd2020CardTypeKnown {
				d.logf("HD2020/Gen6 protokolü algılandı; cardType=0x%02x deviceID=%s realtime bitmap backend kullanılacak", d.hd2020CardType, d.hd2020DeviceID)
			} else {
				d.logf("HD2020/Gen6 protokolü algılandı; realtime bitmap backend kullanılacak")
			}
			return nil
		}
		d.closeInternal()
		return fmt.Errorf("versiyon anlaşma hatası: %w", err)
	}

	// Aşama 2: SDK Version anlaşması
	d.logf("Aşama 2: SDK Version anlaşması")
	if err := d.handshakeSdkVersion(); err != nil {
		d.closeInternal()
		return fmt.Errorf("SDK versiyon anlaşma hatası: %w", err)
	}

	// Aşama 3: Device Info sorgulama
	d.logf("Aşama 3: Cihaz bilgisi sorgulanıyor")
	if err := d.handshakeDeviceInfo(); err != nil {
		// DeviceInfo alınamazsa bağlantıyı kapatma, devam et
		d.logf("UYARI: Cihaz bilgisi alınamadı: %v", err)
	}

	// Heartbeat goroutine'ini başlat
	d.stopHeartbeat = make(chan struct{})
	go d.heartbeatLoop()

	d.logf("Bağlantı başarıyla kuruldu (GUID: %s)", d.sdkGUID)
	return nil
}

// Close, cihaz bağlantısını güvenli bir şekilde kapatır.
// Heartbeat goroutine'i durdurulur ve TCP bağlantısı kapatılır.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closeInternal()
}

// closeInternal, bağlantıyı kapatır (mutex dışında çağrılır).
func (d *Device) closeInternal() error {
	if d.stopHeartbeat != nil {
		close(d.stopHeartbeat)
		d.stopHeartbeat = nil
	}
	d.connected = false
	if d.conn != nil {
		err := d.conn.Close()
		d.conn = nil
		return err
	}
	return nil
}

// IsConnected, bağlantının aktif olup olmadığını döner.
func (d *Device) IsConnected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

// Host, cihazın IP adresini döner.
func (d *Device) Host() string {
	return d.host
}

// Port, cihazın port numarasını döner.
func (d *Device) Port() int {
	return d.port
}

// Protocol, son başarılı Connect çağrısında algılanan protokol ailesini döner.
func (d *Device) Protocol() ProtocolKind {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.protocol
}

// HD2020CardType, HD2020/Gen6 cihazlarında probe yanıtından okunan kart tipini
// döner. Bilgi alınamadıysa ok=false döner.
func (d *Device) HD2020CardType() (cardType byte, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hd2020CardType, d.hd2020CardTypeKnown
}

// HD2020DeviceID, HD2020/Gen6 probe yanıtından okunan cihaz kimliğini döner.
func (d *Device) HD2020DeviceID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hd2020DeviceID
}

// GUID, bu oturumun SDK GUID değerini döner.
// Connect() çağrılmadan önce boş string döner.
func (d *Device) GUID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sdkGUID
}

// CachedDeviceInfo, son handshake'te alınan cihaz bilgilerini döner.
// Güncel bilgi için GetDeviceInfo() kullanın.
func (d *Device) CachedDeviceInfo() *DeviceInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.info
}

// ─── Handshake ──────────────────────────────────────────────────────────────────

// handshakeVersion, transport protocol version anlaşmasını gerçekleştirir.
func (d *Device) handshakeVersion() error {
	// Version paketi gönder
	pkt := buildVersionPacket()
	if err := d.sendRaw(pkt); err != nil {
		return fmt.Errorf("versiyon paketi gönderilemedi: %w", err)
	}

	// Yanıt oku
	data, cmdType, err := d.readPacket()
	if err != nil {
		return fmt.Errorf("versiyon yanıtı okunamadı: %w", err)
	}

	switch cmdType {
	case CmdServiceAnswer:
		ver, ok := parseVersionResponse(data)
		if !ok {
			return fmt.Errorf("versiyon yanıtı çözümlenemedi")
		}
		d.logf("Transport Protocol Version: 0x%08x", ver)
		return nil

	case CmdErrorAnswer:
		errCode, ok := parseErrorCode(data)
		if ok {
			return fmt.Errorf("cihaz hata döndü: %s", errCode)
		}
		return fmt.Errorf("cihaz hata döndü (bilinmeyen format)")

	default:
		return fmt.Errorf("beklenmeyen yanıt tipi: %s", cmdType)
	}
}

// handshakeSdkVersion, SDK versiyon anlaşmasını gerçekleştirir.
// Bu aşamada ##GUID placeholder'ı gerçek GUID ile değiştirilir.
func (d *Device) handshakeSdkVersion() error {
	xmlData := buildVersionXML()
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	// GUID'i kaydet
	d.sdkGUID = resp.GUID
	if d.sdkGUID == "" || d.sdkGUID == "##GUID" {
		// Yanıttan çıkarılamadıysa yeni GUID oluştur
		d.sdkGUID = uuid.New().String()
	}

	d.logf("SDK GUID alındı: %s", d.sdkGUID)
	return nil
}

// handshakeDeviceInfo, cihaz bilgilerini sorgular ve kaydeder.
func (d *Device) handshakeDeviceInfo() error {
	xmlData := buildSdkXML(d.sdkGUID, MethodGetDeviceInfo, "")
	resp, err := d.sendSdkCmdAndReceive([]byte(xmlData))
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("GetDeviceInfo başarısız: %s", resp.Result)
	}

	info, err := parseDeviceInfoXML(resp.InnerXML)
	if err != nil {
		return fmt.Errorf("cihaz bilgisi ayrıştırılamadı: %w", err)
	}

	d.info = info
	d.logf("Cihaz: %s (ID: %s, Ekran: %dx%d)", info.Model, info.DeviceID, info.ScreenWidth, info.ScreenHeight)
	return nil
}

// ─── Veri Gönderme/Alma ─────────────────────────────────────────────────────────

// sendRaw, ham byte verisini TCP bağlantısına yazar.
func (d *Device) sendRaw(data []byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if d.conn == nil {
		return fmt.Errorf("bağlantı kapalı")
	}

	d.conn.SetWriteDeadline(time.Now().Add(d.opts.timeout))
	_, err := d.conn.Write(data)
	return err
}

// sendSdkCmd, XML verisini SDK komut paketlerine dönüştürüp gönderir.
// Büyük XML'ler otomatik olarak parçalara bölünür.
func (d *Device) sendSdkCmd(xmlData []byte) error {
	packets := buildSdkCmdPackets(xmlData)
	for _, pkt := range packets {
		if err := d.sendRaw(pkt); err != nil {
			return err
		}
	}
	return nil
}

// sendSdkCmdAndReceive, SDK komutu gönderir ve yanıtı bekler.
// Bu, en çok kullanılan gönder-al döngüsüdür.
func (d *Device) sendSdkCmdAndReceive(xmlData []byte) (*SdkResponse, error) {
	if err := d.sendSdkCmd(xmlData); err != nil {
		return nil, fmt.Errorf("SDK komutu gönderilemedi: %w", err)
	}

	return d.readSdkResponse()
}

// readPacket, TCP'den bir tam paket okur.
// Huidu protokolünün sticky packet handling mantığını uygular:
//  1. İlk 2 byte okunur → paket uzunluğu
//  2. Kalan byte'lar okunur
//  3. Tam paket döner
//
// Bu fonksiyon, birden fazla paketin tek bir TCP segment'inde
// gelmesi durumunu doğru şekilde ele alır.
func (d *Device) readPacket() ([]byte, CmdType, error) {
	if d.conn == nil {
		return nil, 0, fmt.Errorf("bağlantı kapalı")
	}

	d.conn.SetReadDeadline(time.Now().Add(d.opts.timeout))

	// İlk 2 byte: paket uzunluğu
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(d.conn, lenBuf); err != nil {
		return nil, 0, fmt.Errorf("paket uzunluğu okunamadı: %w", err)
	}
	if isHD2020Gen6Prefix(lenBuf) {
		return nil, 0, fmt.Errorf("%w: HD2020/Gen6 yanıtı alındı (prefix % x); bu Device SDK2 TCP protokolünü destekler, port %d için ayrı HD2020/Gen6 backend kullanın", ErrUnsupportedProtocol, lenBuf, d.port)
	}

	pktLen := int(binary.LittleEndian.Uint16(lenBuf))
	if pktLen < tcpHeaderLength {
		return nil, 0, fmt.Errorf("geçersiz paket uzunluğu: %d", pktLen)
	}

	// Kalan veriyi oku
	pkt := make([]byte, pktLen)
	copy(pkt[0:2], lenBuf)
	if _, err := io.ReadFull(d.conn, pkt[2:]); err != nil {
		return nil, 0, fmt.Errorf("paket verisi okunamadı: %w", err)
	}

	cmdType := CmdType(binary.LittleEndian.Uint16(pkt[2:4]))
	return pkt, cmdType, nil
}

// readSdkResponse, SDK komut yanıtını okur.
// Fragment reassembly: Büyük XML yanıtları birden fazla pakette gelebilir.
// Bu fonksiyon tüm parçaları birleştirir ve tam XML'i döner.
func (d *Device) readSdkResponse() (*SdkResponse, error) {
	var xmlBuf []byte
	var totalExpected uint32

	for {
		data, cmdType, err := d.readPacket()
		if err != nil {
			return nil, err
		}

		switch cmdType {
		case CmdSdkCmdAnswer:
			totalLen, offset, ok := parseSdkCmdHeader(data)
			if !ok {
				return nil, fmt.Errorf("SDK yanıt header'ı çözümlenemedi")
			}

			// İlk parçada buffer oluştur
			if xmlBuf == nil {
				xmlBuf = make([]byte, totalLen)
				totalExpected = totalLen
			}

			// XML verisini kopyala
			xmlChunk := data[sdkCmdHeaderLength:]
			copy(xmlBuf[offset:], xmlChunk)

			// Tüm parçalar alındı mı kontrol et
			if offset+uint32(len(xmlChunk)) >= totalExpected {
				// XML'i temizle ve ayrıştır
				xmlStr := cleanXML(xmlBuf)
				return parseSdkResponse(xmlStr)
			}

		case CmdErrorAnswer:
			errCode, ok := parseErrorCode(data)
			if ok {
				return nil, fmt.Errorf("SDK hata yanıtı: %s", errCode)
			}
			return nil, fmt.Errorf("SDK hata yanıtı (bilinmeyen format)")

		case CmdHeartbeatAnswer:
			// Heartbeat yanıtı geldi, asıl yanıtı beklemeye devam et
			d.logf("Heartbeat yanıtı alındı (SDK yanıt bekleniyor)")
			continue

		default:
			return nil, fmt.Errorf("beklenmeyen yanıt tipi: %s (0x%04x)", cmdType, uint16(cmdType))
		}
	}
}

// ─── Heartbeat ──────────────────────────────────────────────────────────────────

// heartbeatLoop, periyodik heartbeat paketleri gönderen arka plan goroutine'idir.
// Connect() tarafından başlatılır, Close() tarafından durdurulur.
func (d *Device) heartbeatLoop() {
	ticker := time.NewTicker(d.opts.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopHeartbeat:
			return
		case <-ticker.C:
			d.mu.Lock()
			if !d.connected {
				d.mu.Unlock()
				return
			}
			d.mu.Unlock()

			pkt := buildHeartbeat()
			if err := d.sendRaw(pkt); err != nil {
				d.logf("Heartbeat gönderilemedi: %v", err)
				return
			}
			d.logf("Heartbeat gönderildi")
		}
	}
}

// ─── Dahili Yardımcılar ─────────────────────────────────────────────────────────

// logf, yapılandırılmış logger varsa mesaj yazar.
func (d *Device) logf(format string, v ...interface{}) {
	if d.opts.logger != nil {
		d.opts.logger.Printf("[huidu] "+format, v...)
	}
}

// ensureConnected, bağlantının aktif olduğunu kontrol eder.
func (d *Device) ensureConnected() error {
	if !d.connected {
		return fmt.Errorf("cihaz bağlı değil, önce Connect() çağırın")
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return nil
	}
	if d.conn == nil {
		return fmt.Errorf("cihaz bağlı değil, önce Connect() çağırın")
	}
	return nil
}

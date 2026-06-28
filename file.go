package huidu

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ─── Dosya Yükleme ──────────────────────────────────────────────────────────────
//
// Bu dosya, Huidu cihazına dosya yükleme (upload) işlevlerini içerir.
// Dosya yükleme 3 aşamalı bir protokol kullanır:
//
//  1. kFileStartAsk (0x8001): Dosya adı, MD5, boyut ve tip bilgisi gönderilir
//     Cihaz kFileStartAnswer (0x8002) ile yanıt verir (hata kodu + mevcut byte sayısı)
//  2. kFileContentAsk (0x8003): Dosya içeriği MaxContentLength (8000 byte) parçalar halinde gönderilir
//     Her parça için kFileContentAnswer (0x8004) beklenmez (fire-and-forget)
//  3. kFileEndAsk (0x8005): Transfer tamamlandı bildirimi
//     Cihaz kFileEndAnswer (0x8006) ile onaylar

// UploadFile, belirtilen dosyayı cihaza yükler.
// Dosya tipi dosya uzantısından otomatik tespit edilir.
//
//	err := dev.UploadFile("/path/to/image.jpg")
//	if err != nil {
//	    log.Fatal(err)
//	}
func (d *Device) UploadFile(filePath string) error {
	return d.UploadFileAs(filePath, FileTypeAuto)
}

// UploadFileAs, belirtilen dosyayı belirli bir dosya tipiyle cihaza yükler.
//
//	err := dev.UploadFileAs("/path/to/image.jpg", huidu.FileTypeImage)
func (d *Device) UploadFileAs(filePath string, fileType FileType) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return fmt.Errorf("%w: HD2020/Gen6 dosya yükleme bu backend'de desteklenmiyor; metin realtime bitmap olarak gönderilir", ErrUnsupportedProtocol)
	}

	// Dosyayı aç
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("dosya açılamadı: %w", err)
	}
	defer file.Close()

	// Dosya bilgilerini al
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("dosya bilgisi alınamadı: %w", err)
	}

	fileName := filepath.Base(filePath)
	fileSize := stat.Size()

	// Dosya tipini belirle
	if fileType == FileTypeAuto {
		fileType = detectFileType(filePath)
	}

	// MD5 hesapla
	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("MD5 hesaplanamadı: %w", err)
	}
	md5Hash := hex.EncodeToString(hasher.Sum(nil))

	// Dosya başına geri dön
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("dosya konumu sıfırlanamadı: %w", err)
	}

	d.logf("Dosya yükleme başlatılıyor: %s (%d bytes, MD5: %s)", fileName, fileSize, md5Hash)

	// Aşama 1: File Start
	startPkt := buildFileStartPacket(fileName, fileSize, fileType, md5Hash)
	if err := d.sendRaw(startPkt); err != nil {
		return fmt.Errorf("dosya başlatma paketi gönderilemedi: %w", err)
	}

	// File Start yanıtını oku
	data, cmdType, err := d.readPacket()
	if err != nil {
		return fmt.Errorf("dosya başlatma yanıtı okunamadı: %w", err)
	}

	if cmdType != CmdFileStartAnswer {
		return fmt.Errorf("beklenmeyen yanıt tipi: %s (0x%04x)", cmdType, uint16(cmdType))
	}

	errCode, existBytes, ok := parseFileStartResponse(data)
	if !ok {
		return fmt.Errorf("dosya başlatma yanıtı çözümlenemedi")
	}

	if errCode != ErrSuccess {
		return fmt.Errorf("dosya başlatma hatası: %s", errCode)
	}

	// Resume desteği: daha önce gönderilmiş byte'ları atla
	if existBytes > 0 {
		d.logf("Devam ediliyor: %d byte zaten gönderilmiş", existBytes)
		if _, err := file.Seek(int64(existBytes), io.SeekStart); err != nil {
			return fmt.Errorf("dosya konumu ayarlanamadı: %w", err)
		}
	}

	// Aşama 2: File Content (parçalar halinde gönder)
	buf := make([]byte, MaxContentLength)
	sentBytes := int64(existBytes)
	totalBytes := fileSize

	for {
		n, err := file.Read(buf)
		if n > 0 {
			contentPkt := buildFileContentPacket(buf[:n])
			if err := d.sendRaw(contentPkt); err != nil {
				return fmt.Errorf("dosya içeriği gönderilemedi: %w", err)
			}

			sentBytes += int64(n)

			// İlerleme callback'i çağır
			if d.opts.onProgress != nil {
				progress := float64(sentBytes) / float64(totalBytes) * 100
				d.opts.onProgress(UploadProgress{
					FileName:   fileName,
					TotalBytes: totalBytes,
					SentBytes:  sentBytes,
					Percent:    progress,
				})
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("dosya okuma hatası: %w", err)
		}
	}

	// Aşama 3: File End
	endPkt := buildFileEndPacket()
	if err := d.sendRaw(endPkt); err != nil {
		return fmt.Errorf("dosya bitiş paketi gönderilemedi: %w", err)
	}

	// File End yanıtını oku
	data, cmdType, err = d.readPacket()
	if err != nil {
		return fmt.Errorf("dosya bitiş yanıtı okunamadı: %w", err)
	}

	if cmdType != CmdFileEndAnswer {
		return fmt.Errorf("beklenmeyen yanıt tipi: %s (0x%04x)", cmdType, uint16(cmdType))
	}

	endErrCode, ok := parseFileEndResponse(data)
	if !ok {
		return fmt.Errorf("dosya bitiş yanıtı çözümlenemedi")
	}

	if endErrCode != ErrSuccess && endErrCode != ErrWriteFinish {
		return fmt.Errorf("dosya bitiş hatası: %s", endErrCode)
	}

	d.logf("Dosya başarıyla yüklendi: %s (%d bytes)", fileName, totalBytes)
	return nil
}

// UploadFileData, bellek içi veriyi dosya olarak cihaza yükler.
// Dosya sistemi kullanmadan doğrudan byte verisi yüklemek için kullanılır.
//
//	data := []byte("...ikili veri...")
//	err := dev.UploadFileData("dynamic.jpg", data, huidu.FileTypeImage)
func (d *Device) UploadFileData(fileName string, fileData []byte, fileType FileType) error {
	if err := d.ensureConnected(); err != nil {
		return err
	}
	if d.protocol == ProtocolHD2020Gen6 {
		return fmt.Errorf("%w: HD2020/Gen6 dosya yükleme bu backend'de desteklenmiyor; metin realtime bitmap olarak gönderilir", ErrUnsupportedProtocol)
	}

	fileSize := int64(len(fileData))

	// Dosya tipini belirle
	if fileType == FileTypeAuto {
		fileType = detectFileType(fileName)
	}

	// MD5 hesapla
	hasher := md5.New()
	hasher.Write(fileData)
	md5Hash := hex.EncodeToString(hasher.Sum(nil))

	d.logf("Bellek verisi yükleniyor: %s (%d bytes)", fileName, fileSize)

	// Aşama 1: File Start
	startPkt := buildFileStartPacket(fileName, fileSize, fileType, md5Hash)
	if err := d.sendRaw(startPkt); err != nil {
		return fmt.Errorf("dosya başlatma paketi gönderilemedi: %w", err)
	}

	data, cmdType, err := d.readPacket()
	if err != nil {
		return fmt.Errorf("dosya başlatma yanıtı okunamadı: %w", err)
	}

	if cmdType != CmdFileStartAnswer {
		return fmt.Errorf("beklenmeyen yanıt tipi: %s", cmdType)
	}

	errCode, existBytes, ok := parseFileStartResponse(data)
	if !ok {
		return fmt.Errorf("dosya başlatma yanıtı çözümlenemedi")
	}

	if errCode != ErrSuccess {
		return fmt.Errorf("dosya başlatma hatası: %s", errCode)
	}

	// Aşama 2: File Content
	offset := int(existBytes)
	for offset < len(fileData) {
		end := offset + MaxContentLength
		if end > len(fileData) {
			end = len(fileData)
		}

		contentPkt := buildFileContentPacket(fileData[offset:end])
		if err := d.sendRaw(contentPkt); err != nil {
			return fmt.Errorf("dosya içeriği gönderilemedi: %w", err)
		}

		offset = end

		if d.opts.onProgress != nil {
			progress := float64(offset) / float64(fileSize) * 100
			d.opts.onProgress(UploadProgress{
				FileName:   fileName,
				TotalBytes: fileSize,
				SentBytes:  int64(offset),
				Percent:    progress,
			})
		}
	}

	// Aşama 3: File End
	endPkt := buildFileEndPacket()
	if err := d.sendRaw(endPkt); err != nil {
		return fmt.Errorf("dosya bitiş paketi gönderilemedi: %w", err)
	}

	data, cmdType, err = d.readPacket()
	if err != nil {
		return fmt.Errorf("dosya bitiş yanıtı okunamadı: %w", err)
	}

	if cmdType != CmdFileEndAnswer {
		return fmt.Errorf("beklenmeyen yanıt tipi: %s", cmdType)
	}

	endErrCode, ok := parseFileEndResponse(data)
	if !ok {
		return fmt.Errorf("dosya bitiş yanıtı çözümlenemedi")
	}

	if endErrCode != ErrSuccess && endErrCode != ErrWriteFinish {
		return fmt.Errorf("dosya bitiş hatası: %s", endErrCode)
	}

	d.logf("Veri başarıyla yüklendi: %s (%d bytes)", fileName, fileSize)
	return nil
}

// ─── Dosya Tipi Tespiti ─────────────────────────────────────────────────────────

// detectFileType, dosya uzantısından dosya tipini otomatik tespit eder.
// C# SDK'daki GetHFileType fonksiyonuyla aynı mantığı kullanır.
func detectFileType(filePath string) FileType {
	ext := strings.ToLower(filepath.Ext(filePath))
	name := strings.ToLower(filepath.Base(filePath))

	// Görsel uzantıları
	imageExts := map[string]bool{
		".bmp": true, ".jpg": true, ".jpeg": true, ".png": true,
		".ico": true, ".gif": true, ".tif": true, ".tiff": true,
	}

	// Video uzantıları
	videoExts := map[string]bool{
		".mp4": true, ".avi": true, ".mkv": true, ".flv": true,
		".mov": true, ".wmv": true, ".mp3": true, ".swf": true,
		".f4v": true, ".trp": true, ".asf": true, ".mpeg": true,
		".webm": true, ".asx": true, ".rm": true, ".rmvb": true,
		".3gp": true, ".m4v": true, ".dat": true, ".vob": true,
		".ts": true,
	}

	// Font uzantıları
	fontExts := map[string]bool{
		".ttf": true, ".ttc": true, ".bdf": true,
	}

	// Firmware uzantıları
	firmwareExts := map[string]bool{
		".bin": true,
	}

	switch {
	case imageExts[ext]:
		return FileTypeImage
	case videoExts[ext]:
		return FileTypeVideo
	case fontExts[ext]:
		return FileTypeFont
	case firmwareExts[ext]:
		return FileTypeFirmware
	case ext == ".xml":
		// Özel XML dosyaları
		if name == "fpga.xml" {
			return FileTypeFPGAConfig
		}
		if name == "config.xml" {
			return FileTypeSettingConfig
		}
		return FileTypeProgramXML
	default:
		return FileTypeImage // Varsayılan
	}
}

// ─── Toplu Dosya Yükleme ────────────────────────────────────────────────────────

// UploadFiles, birden fazla dosyayı sırayla cihaza yükler.
// Her dosya için UploadFile çağrılır.
//
//	err := dev.UploadFiles("/path/to/img1.jpg", "/path/to/img2.png")
func (d *Device) UploadFiles(filePaths ...string) error {
	for _, path := range filePaths {
		if err := d.UploadFile(path); err != nil {
			return fmt.Errorf("dosya yükleme hatası (%s): %w", path, err)
		}
	}
	return nil
}

// ─── Dosya MD5 Hesaplama ────────────────────────────────────────────────────────

// FileMD5, dosyanın MD5 hash'ini hesaplar.
// Yardımcı fonksiyondur, doğrudan cihazla iletişim kurmaz.
//
//	hash, err := huidu.FileMD5("/path/to/file.jpg")
//	fmt.Println(hash) // "d41d8cd98f00b204e9800998ecf8427e"
func FileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

package huidu

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
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
	if spec.width != hd2020RealtimeWidth || spec.height != hd2020RealtimeHeight {
		return fmt.Errorf("%w: HD2020/Gen6 realtime backend şu anda sadece %dx%d alan destekliyor (istenen: %dx%d)",
			ErrUnsupportedProtocol, hd2020RealtimeWidth, hd2020RealtimeHeight, spec.width, spec.height)
	}

	bitmap, err := renderHD2020TextBitmap(spec.text, spec.width, spec.height)
	if err != nil {
		return err
	}

	transferID := uint16(time.Now().UnixNano())
	packets := buildHD2020RealtimeAreaPackets(bitmap, transferID)
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

func renderHD2020TextBitmap(text string, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || width%8 != 0 {
		return nil, fmt.Errorf("geçersiz HD2020 bitmap boyutu: %dx%d", width, height)
	}

	text = strings.TrimSpace(strings.ToUpper(text))
	if text == "" {
		text = " "
	}

	img := image.NewAlpha(image.Rect(0, 0, width, height))
	face := basicfont.Face7x13
	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Alpha{A: 255}),
		Face: face,
	}

	textWidth := drawer.MeasureString(text).Ceil()
	if textWidth > width {
		textWidth = width
	}
	metrics := face.Metrics()
	textHeight := metrics.Height.Ceil()
	x := (width - textWidth) / 2
	if x < 0 {
		x = 0
	}
	y := (height-textHeight)/2 + metrics.Ascent.Ceil()
	if y < metrics.Ascent.Ceil() {
		y = metrics.Ascent.Ceil()
	}

	drawer.Dot = fixed.P(x, y)
	drawer.DrawString(text)

	rowBytes := width / 8
	// HD2020 realtime-area image data duplicates each logical row vertically.
	bitmap := make([]byte, rowBytes*height*2)
	for py := 0; py < height; py++ {
		for px := 0; px < width; px++ {
			if img.AlphaAt(px, py).A == 0 {
				continue
			}
			for dup := 0; dup < 2; dup++ {
				row := py*2 + dup
				idx := row*rowBytes + px/8
				bitmap[idx] |= 0x80 >> uint(px%8)
			}
		}
	}

	return bitmap, nil
}

func buildHD2020RealtimeAreaPackets(bitmap []byte, transferID uint16) [][]byte {
	startPayload := make([]byte, 47)
	startPayload[0] = 0x2f
	binary.LittleEndian.PutUint16(startPayload[1:3], transferID)
	startPayload[4] = 0x01
	startPayload[6] = 0x01

	dataPayload := make([]byte, 5+52+len(bitmap))
	dataPayload[0] = 0x05
	binary.LittleEndian.PutUint16(dataPayload[1:3], transferID)
	copy(dataPayload[5:], hd2020AreaHeader(hd2020RealtimeWidth, hd2020RealtimeHeight))
	copy(dataPayload[57:], bitmap)

	endPayload := make([]byte, 3)
	endPayload[0] = 0x03
	binary.LittleEndian.PutUint16(endPayload[1:3], transferID)

	return [][]byte{
		buildHD2020Packet(0x18, 1, startPayload),
		buildHD2020Packet(0x19, 2, dataPayload),
		buildHD2020Packet(0x1a, 3, endPayload),
	}
}

func hd2020AreaHeader(width, height int) []byte {
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
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 31)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("HD2020/Gen6 yanıtı okunamadı: %w", err)
	}
	if len(buf) < 22 || buf[0] != 'H' || buf[1] != 'R' {
		return fmt.Errorf("HD2020/Gen6 beklenmeyen yanıt")
	}
	if buf[20] != expectedCmd {
		return fmt.Errorf("HD2020/Gen6 beklenmeyen yanıt komutu: got 0x%02x want 0x%02x", buf[20], expectedCmd)
	}
	if buf[21] != 0x00 {
		return fmt.Errorf("HD2020/Gen6 komut hatası (cmd=0x%02x, status=0x%02x)", expectedCmd, buf[21])
	}
	return nil
}

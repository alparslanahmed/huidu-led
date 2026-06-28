package huidu

import "testing"

func TestHD2020RealtimeAreaPacketsShapeAndChecksum(t *testing.T) {
	bitmap := make([]byte, 512)
	for i := range bitmap {
		bitmap[i] = byte(i)
	}

	packets := buildHD2020RealtimeAreaPackets(bitmap, 64, 32, 2, 0x1234)
	if len(packets) != 3 {
		t.Fatalf("packet count = %d, want 3", len(packets))
	}

	tests := []struct {
		name string
		pkt  []byte
		cmd  byte
		seq  byte
		size int
	}{
		{"start", packets[0], 0x18, 1, 77},
		{"data", packets[1], 0x19, 2, 599},
		{"end", packets[2], 0x1a, 3, 33},
	}

	for _, tt := range tests {
		if len(tt.pkt) != tt.size {
			t.Fatalf("%s len = %d, want %d", tt.name, len(tt.pkt), tt.size)
		}
		if tt.pkt[0] != 'H' || tt.pkt[1] != 'T' {
			t.Fatalf("%s magic = % x, want HT", tt.name, tt.pkt[:2])
		}
		if tt.pkt[6] != tt.cmd || tt.pkt[26] != tt.seq {
			t.Fatalf("%s cmd/seq = 0x%02x/%d, want 0x%02x/%d", tt.name, tt.pkt[6], tt.pkt[26], tt.cmd, tt.seq)
		}
		assertHD2020Checksum(t, tt.name, tt.pkt)
	}

	if got := packets[1][84:596]; len(got) != len(bitmap) {
		t.Fatalf("data bitmap len = %d, want %d", len(got), len(bitmap))
	}
	if packets[1][28] != 0x34 || packets[1][29] != 0x12 {
		t.Fatalf("transfer id bytes = %02x %02x, want 34 12", packets[1][28], packets[1][29])
	}
	if packets[1][41] != 0x40 || packets[1][43] != 0x20 || packets[1][82] != 0x02 {
		t.Fatalf("legacy area header width/height/planes = %02x/%02x/%02x, want 40/20/02", packets[1][41], packets[1][43], packets[1][82])
	}
}

func TestRenderHD2020TextBitmap(t *testing.T) {
	bitmap, err := renderHD2020TextBitmap("34ABC123", hd2020RealtimeWidth, hd2020RealtimeHeight)
	if err != nil {
		t.Fatalf("renderHD2020TextBitmap() error = %v", err)
	}
	if len(bitmap) != 512 {
		t.Fatalf("bitmap len = %d, want 512", len(bitmap))
	}

	var lit int
	for _, b := range bitmap {
		if b != 0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("bitmap is blank")
	}
}

func TestEncodeHD2020FullColorRGBBitmap(t *testing.T) {
	mask, err := renderHD2020TextMask("GECIS 41RL207", 128, 64, TextConfig{HAlign: HAlignCenter, VAlign: VAlignMiddle})
	if err != nil {
		t.Fatalf("renderHD2020TextMask() error = %v", err)
	}
	var maskLit int
	for _, on := range mask {
		if on {
			maskLit++
		}
	}
	if maskLit < 700 {
		t.Fatalf("scaled text is too small: lit pixels = %d", maskLit)
	}

	bitmap, planes, err := encodeHD2020TextBitmap(mask, 128, 64, ColorWhite, "#22c55e", hd2020BitmapFullColorRGB)
	if err != nil {
		t.Fatalf("encodeHD2020TextBitmap() error = %v", err)
	}
	if planes != 3 {
		t.Fatalf("planes = %d, want 3", planes)
	}
	if len(bitmap) != 3072 {
		t.Fatalf("bitmap len = %d, want 3072", len(bitmap))
	}

	var lit int
	for _, b := range bitmap {
		if b != 0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("bitmap is blank")
	}
	rowBytes := 128 / 8
	if bitmap[rowBytes] != 0xff {
		t.Fatalf("green background plane was not filled")
	}
	blackBitmap, _, err := encodeHD2020TextBitmap(mask, 128, 64, "#000000", "#22c55e", hd2020BitmapFullColorRGB)
	if err != nil {
		t.Fatalf("encode black-on-green bitmap: %v", err)
	}
	var greenBytes, clearedGreenBytes int
	for py := 0; py < 64; py++ {
		rowBase := py * rowBytes * 3
		for _, b := range blackBitmap[rowBase+rowBytes : rowBase+rowBytes*2] {
			if b != 0 {
				greenBytes++
			}
			if b != 0xff {
				clearedGreenBytes++
			}
		}
	}
	if greenBytes == 0 {
		t.Fatalf("green background unexpectedly blank")
	}
	if clearedGreenBytes == 0 {
		t.Fatalf("black text did not clear any green background pixels")
	}

	packets := buildHD2020RealtimeAreaPackets(bitmap, 128, 64, planes, 0x9876)
	if len(packets) != 9 {
		t.Fatalf("full-color packet count = %d, want 9", len(packets))
	}
	if packets[0][31] != 0x07 {
		t.Fatalf("full-color chunk count = %d, want 7", packets[0][31])
	}
	if len(packets[1]) != 547 {
		t.Fatalf("full-color first data packet len = %d, want 547", len(packets[1]))
	}
	if packets[1][41] != 0x80 || packets[1][43] != 0x40 || packets[1][82] != 0x03 {
		t.Fatalf("full-color area header width/height/planes = %02x/%02x/%02x, want 80/40/03", packets[1][41], packets[1][43], packets[1][82])
	}
	if len(packets[7]) != 87 {
		t.Fatalf("full-color final data packet len = %d, want 87", len(packets[7]))
	}
	for i, packet := range packets {
		assertHD2020Checksum(t, "full-color packet", packet)
		if i >= 1 && i <= 7 {
			if packet[30] != 0x00 || packet[31] != byte(i-1) {
				t.Fatalf("full-color packet %d chunk index = %02x %02x, want 00 %02x", i, packet[30], packet[31], i-1)
			}
		}
	}
}

func TestEncodeHD2020ProgramBitmap(t *testing.T) {
	mask := make([]bool, 128*64)
	mask[0] = true
	mask[127] = true

	bitmap, err := encodeHD2020ProgramBitmap(mask, 128, 64, "#ffffff", "#22c55e")
	if err != nil {
		t.Fatalf("encodeHD2020ProgramBitmap() error = %v", err)
	}
	if len(bitmap) != 2048 {
		t.Fatalf("bitmap len = %d, want 2048", len(bitmap))
	}

	rowBytes := 128 / 8
	if bitmap[0] != 0x80 {
		t.Fatalf("red/text plane first byte = %02x, want 80", bitmap[0])
	}
	if bitmap[rowBytes-1] != 0x01 {
		t.Fatalf("red/text plane last byte = %02x, want 01", bitmap[rowBytes-1])
	}
	for i, b := range bitmap[rowBytes : rowBytes*2] {
		if b != 0xff {
			t.Fatalf("green/background plane byte %d = %02x, want ff", i, b)
		}
	}

	blackBitmap, err := encodeHD2020ProgramBitmap(mask, 128, 64, "#000000", "#f59e0b")
	if err != nil {
		t.Fatalf("encode black-on-amber program bitmap: %v", err)
	}
	if blackBitmap[0] != 0x7f || blackBitmap[rowBytes] != 0x7f {
		t.Fatalf("black text did not clear amber background: red=%02x green=%02x", blackBitmap[0], blackBitmap[rowBytes])
	}
}

func TestHD2020ProgramScreenPacketsMatchSDKShape(t *testing.T) {
	bitmap := make([]byte, 2048)
	for i := range bitmap {
		bitmap[i] = byte(i)
	}

	packets := buildHD2020ProgramScreenPackets(bitmap, 128, 64, 0x9735)
	if len(packets) != 6 {
		t.Fatalf("packet count = %d, want 6", len(packets))
	}

	tests := []struct {
		name string
		pkt  []byte
		cmd  byte
		seq  byte
		size int
	}{
		{"screen params", packets[0], 0x13, 1, 96},
		{"start", packets[1], 0x02, 2, 35},
		{"data 0", packets[2], 0x03, 3, 1035},
		{"data 1", packets[3], 0x03, 4, 1035},
		{"data 2", packets[4], 0x03, 5, 227},
		{"end", packets[5], 0x04, 6, 33},
	}
	for _, tt := range tests {
		if len(tt.pkt) != tt.size {
			t.Fatalf("%s len = %d, want %d", tt.name, len(tt.pkt), tt.size)
		}
		if tt.pkt[6] != tt.cmd || tt.pkt[26] != tt.seq {
			t.Fatalf("%s cmd/seq = 0x%02x/%d, want 0x%02x/%d", tt.name, tt.pkt[6], tt.pkt[26], tt.cmd, tt.seq)
		}
		assertHD2020Checksum(t, tt.name, tt.pkt)
	}

	screenPayload := packets[0][27 : len(packets[0])-3]
	if screenPayload[5] != 0x80 || screenPayload[7] != 0x40 {
		t.Fatalf("program screen width/height = %02x/%02x, want 80/40", screenPayload[5], screenPayload[7])
	}
	if screenPayload[59] != 0x01 || screenPayload[60] != 0x01 {
		t.Fatalf("program screen payload mode bytes = %02x/%02x, want 01/01", screenPayload[59], screenPayload[60])
	}

	startPayload := packets[1][27 : len(packets[1])-3]
	if startPayload[0] != 0x05 || startPayload[1] != 0x35 || startPayload[2] != 0x97 || startPayload[4] != 0x03 {
		t.Fatalf("program start payload = % x", startPayload)
	}

	var chunks [][]byte
	for i := 2; i <= 4; i++ {
		payload := packets[i][27 : len(packets[i])-3]
		if payload[0] != 0x05 || payload[1] != 0x35 || payload[2] != 0x97 || payload[4] != byte(i-2) {
			t.Fatalf("program data payload %d prefix = % x", i-2, payload[:5])
		}
		chunks = append(chunks, payload[5:])
	}
	data := append(append(chunks[0], chunks[1]...), chunks[2]...)
	if len(data) != 2192 {
		t.Fatalf("program data len = %d, want 2192", len(data))
	}
	if string(data[0:2]) != "HC" || string(data[29:31]) != "HP" || string(data[89:91]) != "HA" {
		t.Fatalf("program data markers missing")
	}
	if data[98] != 0x80 || data[100] != 0x40 {
		t.Fatalf("area header width/height = %02x/%02x, want 80/40", data[98], data[100])
	}
	if len(data[144:]) != len(bitmap) {
		t.Fatalf("program bitmap len = %d, want %d", len(data[144:]), len(bitmap))
	}
}

func TestHD2020RealtimeScreenSetupPackets(t *testing.T) {
	packets := buildHD2020RealtimeScreenSetupPackets(128, 64, 0xd693)
	if len(packets) != 5 {
		t.Fatalf("setup packet count = %d, want 5", len(packets))
	}

	checks := []struct {
		name string
		cmd  byte
		seq  byte
	}{
		{"open setup", 0x12, 1},
		{"screen params", 0x13, 2},
		{"area start", 0x18, 3},
		{"area data", 0x19, 4},
		{"area end", 0x1a, 5},
	}
	for i, check := range checks {
		packet := packets[i]
		if packet[6] != check.cmd {
			t.Fatalf("%s cmd = 0x%02x, want 0x%02x", check.name, packet[6], check.cmd)
		}
		if packet[26] != check.seq {
			t.Fatalf("%s seq = %d, want %d", check.name, packet[26], check.seq)
		}
		assertHD2020Checksum(t, check.name, packet)
	}

	screenPayload := packets[1][27 : len(packets[1])-3]
	if string(screenPayload[:2]) != "HS" {
		t.Fatalf("screen payload prefix = %q, want HS", screenPayload[:2])
	}
	if screenPayload[5] != 0x80 || screenPayload[7] != 0x40 {
		t.Fatalf("screen setup width/height = %02x/%02x, want 80/40", screenPayload[5], screenPayload[7])
	}

	areaPayload := packets[3][27 : len(packets[3])-3]
	if string(areaPayload[5:7]) != "HL" || string(areaPayload[15:17]) != "HA" {
		t.Fatalf("area setup prefixes = %q/%q, want HL/HA", areaPayload[5:7], areaPayload[15:17])
	}
	if areaPayload[24] != 0x80 || areaPayload[26] != 0x40 {
		t.Fatalf("area setup width/height = %02x/%02x, want 80/40", areaPayload[24], areaPayload[26])
	}
}

func TestParseHD2020ProbeInfo(t *testing.T) {
	resp := []byte{
		0x48, 0x52, 0x0e, 0xff, 0x43, 0x7f, 0x9d, 0x5c,
		0xbe, 0xd0, 0xe3, 0x4d, 0x3f, 0x56, 0xcd, 0xfd,
		0x0b, 0x36, 0x00, 0x1c, 0x1b, 0x04, 0x00, 0x00,
		0x0b, 0x76, 0x87, 0xa2, 0x0a, 0xa5, 0xaa,
	}
	info, ok := parseHD2020ProbeInfo(resp)
	if !ok {
		t.Fatal("parseHD2020ProbeInfo() ok = false")
	}
	if info.cardType != 0x87 {
		t.Fatalf("cardType = 0x%02x, want 0x87", info.cardType)
	}
	if info.deviceID != "7f43ff0e-5c9d-d0be-e34d-3f56cdfd0b36" {
		t.Fatalf("deviceID = %q", info.deviceID)
	}
}

func assertHD2020Checksum(t *testing.T, name string, packet []byte) {
	t.Helper()
	sum := 0
	for _, b := range packet[:len(packet)-3] {
		sum += int(b)
	}
	if packet[len(packet)-3] != byte(sum>>8) || packet[len(packet)-2] != byte(sum) {
		t.Fatalf("%s checksum = %02x %02x, want %02x %02x",
			name,
			packet[len(packet)-3],
			packet[len(packet)-2],
			byte(sum>>8),
			byte(sum),
		)
	}
	if packet[len(packet)-1] != 0xaa {
		t.Fatalf("%s terminator = %02x, want aa", name, packet[len(packet)-1])
	}
}

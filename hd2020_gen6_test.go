package huidu

import "testing"

func TestHD2020RealtimeAreaPacketsShapeAndChecksum(t *testing.T) {
	bitmap := make([]byte, 512)
	for i := range bitmap {
		bitmap[i] = byte(i)
	}

	packets := buildHD2020RealtimeAreaPackets(bitmap, 0x1234)
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

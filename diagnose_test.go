package huidu

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

func TestClassifyProbeResponseSDK2(t *testing.T) {
	pkt := make([]byte, 8)
	binary.LittleEndian.PutUint16(pkt[0:2], 8)
	binary.LittleEndian.PutUint16(pkt[2:4], uint16(CmdServiceAnswer))
	binary.LittleEndian.PutUint32(pkt[4:8], transportVersion)

	if got := classifyProbeResponse(pkt); got != ProtocolSDK2TCP {
		t.Fatalf("classifyProbeResponse() = %q, want %q", got, ProtocolSDK2TCP)
	}
}

func TestClassifyProbeResponseHD2020Gen6(t *testing.T) {
	pkt := []byte{
		0x48, 0x52, 0x0e, 0xff, 0x43, 0x7f, 0x9d, 0x5c,
		0xbe, 0xd0, 0xe3, 0x4d, 0x3f, 0x56, 0xcd, 0xfd,
		0x0b, 0x36, 0x00, 0x1c, 0x00, 0x04, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x08, 0xe0, 0xaa,
	}

	if got := classifyProbeResponse(pkt); got != ProtocolHD2020Gen6 {
		t.Fatalf("classifyProbeResponse() = %q, want %q", got, ProtocolHD2020Gen6)
	}
}

func TestReadPacketHD2020Gen6ReturnsUnsupportedProtocol(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	dev := &Device{
		port: 6101,
		conn: client,
		opts: defaultDeviceOptions(),
	}

	errCh := make(chan error, 1)
	go func() {
		_, _, err := dev.readPacket()
		errCh <- err
	}()

	if _, err := server.Write([]byte{'H', 'R'}); err != nil {
		t.Fatalf("server.Write() error = %v", err)
	}

	if err := <-errCh; !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("readPacket() error = %v, want ErrUnsupportedProtocol", err)
	}
}

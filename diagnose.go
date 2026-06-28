package huidu

import (
	"fmt"
	"net"
	"time"
)

// ProtocolKind identifies the Huidu wire protocol family observed during a
// probe.
type ProtocolKind string

const (
	// ProtocolUnknown means the device did not reply, or the reply was not a
	// recognized Huidu packet.
	ProtocolUnknown ProtocolKind = "unknown"

	// ProtocolSDK2TCP is the XML-over-TCP SDK 2.0 protocol implemented by Device.
	ProtocolSDK2TCP ProtocolKind = "huidu-sdk2-tcp"

	// ProtocolHD2020Gen6 is the HD2020 / Gen6 single-dual color SDK family
	// commonly served on port 6101. It is not compatible with ProtocolSDK2TCP.
	ProtocolHD2020Gen6 ProtocolKind = "huidu-hd2020-gen6"
)

// ProtocolProbeTransport contains the result for one transport.
type ProtocolProbeTransport struct {
	Reachable  bool
	Protocol   ProtocolKind
	Response   []byte
	RemoteAddr string
	Error      string
}

// ProtocolProbeResult contains TCP and UDP probe results for a device address.
type ProtocolProbeResult struct {
	Host string
	Port int
	TCP  ProtocolProbeTransport
	UDP  ProtocolProbeTransport
}

// ProbeProtocol sends non-mutating handshake/discovery frames and classifies the
// controller protocol family. It is intended for diagnostics and for selecting a
// backend without breaking existing SDK2 devices.
func ProbeProtocol(host string, port int, timeout time.Duration) ProtocolProbeResult {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return ProtocolProbeResult{
		Host: host,
		Port: port,
		TCP:  probeTCPProtocol(host, port, timeout),
		UDP:  probeUDPProtocol(host, port, timeout),
	}
}

func probeTCPProtocol(host string, port int, timeout time.Duration) ProtocolProbeTransport {
	var result ProtocolProbeTransport

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), timeout)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer conn.Close()

	result.Reachable = true
	result.RemoteAddr = conn.RemoteAddr().String()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		result.Error = err.Error()
		return result
	}

	if _, err := conn.Write(buildVersionPacket()); err != nil {
		result.Error = err.Error()
		return result
	}

	result.Response, err = readProbeResponse(conn)
	if err != nil {
		result.Error = err.Error()
	}
	result.Protocol = classifyProbeResponse(result.Response)
	return result
}

func probeUDPProtocol(host string, port int, timeout time.Duration) ProtocolProbeTransport {
	var result ProtocolProbeTransport

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		result.Error = err.Error()
		return result
	}

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		result.Error = err.Error()
		return result
	}

	if _, err := conn.WriteToUDP(buildUDPScanPacket(), remoteAddr); err != nil {
		result.Error = err.Error()
		return result
	}

	buf := make([]byte, 512)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Reachable = true
	result.RemoteAddr = addr.String()
	result.Response = append([]byte(nil), buf[:n]...)
	result.Protocol = classifyProbeResponse(result.Response)
	return result
}

func readProbeResponse(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if n > 0 {
		return append([]byte(nil), buf[:n]...), err
	}
	return nil, err
}

func classifyProbeResponse(data []byte) ProtocolKind {
	if len(data) >= 2 && isHD2020Gen6Prefix(data[:2]) {
		return ProtocolHD2020Gen6
	}

	if len(data) >= tcpHeaderLength {
		_, cmdType, ok := parsePacketHeader(data)
		if ok {
			switch cmdType {
			case CmdSearchDeviceAnswer, CmdErrorAnswer, CmdServiceAnswer, CmdSdkCmdAnswer:
				return ProtocolSDK2TCP
			}
		}
	}

	return ProtocolUnknown
}

func isHD2020Gen6Prefix(prefix []byte) bool {
	return len(prefix) >= 2 && prefix[0] == 'H' && prefix[1] == 'R'
}

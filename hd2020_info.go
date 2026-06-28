package huidu

import "fmt"

type hd2020ProbeInfo struct {
	deviceID string
	cardType byte
}

func (d *Device) populateHD2020InfoLocked() {
	result := probeTCPProtocol(d.host, d.port, d.opts.timeout)
	info, ok := parseHD2020ProbeInfo(result.Response)
	if !ok {
		if result.Error != "" {
			d.logf("HD2020/Gen6 probe bilgisi alınamadı: %s", result.Error)
		}
		return
	}

	d.hd2020CardType = info.cardType
	d.hd2020CardTypeKnown = true
	d.hd2020DeviceID = info.deviceID
}

func parseHD2020ProbeInfo(data []byte) (hd2020ProbeInfo, bool) {
	if len(data) < 27 || !isHD2020Gen6Prefix(data[:2]) {
		return hd2020ProbeInfo{}, false
	}

	return hd2020ProbeInfo{
		deviceID: formatHD2020DeviceID(data[2:18]),
		cardType: data[26],
	}, true
}

func formatHD2020DeviceID(raw []byte) string {
	if len(raw) < 16 {
		return ""
	}
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		raw[3], raw[2], raw[1], raw[0],
		raw[5], raw[4],
		raw[7], raw[6],
		raw[8], raw[9],
		raw[10], raw[11], raw[12], raw[13], raw[14], raw[15],
	)
}

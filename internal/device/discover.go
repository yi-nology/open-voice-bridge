package device

// Discovered is a Bluetooth input device currently visible to the kernel.
type Discovered struct {
	Address      string
	Name         string
	Vendor       string
	Product      string
	Profile      string
	Capabilities []string
}

// parseInputDevices extracts BLE input devices from /proc/bus/input/devices.
func parseInputDevices(data string) []Discovered {
	var out []Discovered
	seen := map[string]bool{}
	for _, block := range splitBlocks(data) {
		if !isBluetoothBlock(block) {
			continue
		}
		name := blockValue(block, `N: Name="`, `"`)
		if name == "" || name == "xiaomi-remote virtual keyboard" || name == "keyd virtual keyboard" {
			continue
		}
		address := blockValue(block, "U: Uniq=", "\n")
		if address == "" {
			continue
		}
		address = upperMAC(address)
		if seen[address] {
			continue
		}
		vendor, product := inputIDs(block)
		item := Discovered{
			Address: address,
			Name:    name,
			Vendor:  vendor,
			Product: product,
		}
		if profile, ok := Builtin().MatchInputBlock(block); ok {
			item.Profile = profile.ID
			item.Capabilities = profile.Capabilities
		}
		seen[address] = true
		out = append(out, item)
	}
	return out
}

func splitBlocks(data string) []string {
	var blocks []string
	start := 0
	for index := 0; index < len(data); index++ {
		if data[index] == '\n' && index+1 < len(data) && data[index+1] == '\n' {
			blocks = append(blocks, data[start:index])
			start = index + 2
			index++
		}
	}
	if start < len(data) {
		blocks = append(blocks, data[start:])
	}
	return blocks
}

func isBluetoothBlock(block string) bool {
	return containsFold(block, "bus=0005") && containsFold(block, "handlers=")
}

func inputIDs(block string) (string, string) {
	vendor := cutToken(blockValueFold(block, "vendor=", "\n"))
	product := cutToken(blockValueFold(block, "product=", "\n"))
	return vendor, product
}

func cutToken(value string) string {
	for index, char := range value {
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			return value[:index]
		}
	}
	return value
}

func blockValue(block, prefix, suffix string) string {
	start := indexOf(block, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := indexOf(block[start:], suffix)
	if end < 0 {
		return ""
	}
	return block[start : start+end]
}

func blockValueFold(block, prefix, suffix string) string {
	lower := toLower(block)
	start := indexOf(lower, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := indexOf(lower[start:], suffix)
	if end < 0 {
		return ""
	}
	return toLower(block[start : start+end])
}

func indexOf(value, needle string) int {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

func containsFold(value, needle string) bool {
	return indexOf(toLower(value), needle) >= 0
}

func toLower(value string) string {
	out := []byte(value)
	for index, char := range out {
		if char >= 'A' && char <= 'Z' {
			out[index] = char + ('a' - 'A')
		}
	}
	return string(out)
}

func upperMAC(value string) string {
	out := []byte(value)
	for index, char := range out {
		if char >= 'a' && char <= 'z' {
			out[index] = char - ('a' - 'A')
		}
	}
	return string(out)
}

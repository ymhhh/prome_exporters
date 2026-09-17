//go:build windows

package internal

func lookupIP() string {
	// Prefer interface enumeration on Windows to avoid shelling out.
	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}

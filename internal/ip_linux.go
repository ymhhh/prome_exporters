//go:build linux

package internal

// lookupIP prefers the default-route source IP. `hostname -I` is not used:
// its token order is unstable and often returns docker/VPN/bridge addresses.
func lookupIP() string {
	if ip := firstIPFromDefaultRoute(); ip != "" {
		return ip
	}
	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}

// Package netaddress owns the shared public-address boundary for outbound I/O.
package netaddress

import "net/netip"

// IsPublic reports whether an address is globally routable, excluding CGNAT.
func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is4() {
		ip := addr.As4()
		return !(ip[0] == 100 && ip[1]&0xc0 == 0x40)
	}
	return true
}

package clipboard

import (
	"bytes"
	"net"
	"strconv"

	"golang.design/x/x11"
)

// Match the connected address, not a hostname guessed before DNS resolution.
// Loopback TCP can also use local-family records (as Xlib does); unrelated
// hosts never inherit a local or arbitrary same-display-number credential.
func chooseCookie(entries []x11.AuthEntry, number int, peer net.Addr, hostname string) (string, []byte) {
	local := false
	var ip net.IP
	switch address := peer.(type) {
	case *net.UnixAddr:
		local = true
	case *net.TCPAddr:
		ip = address.IP
		local = ip.IsLoopback()
	}
	want := strconv.Itoa(number)
	best := 100
	var cookie []byte
	for _, e := range entries {
		if e.Name != x11.MITCookie || (e.Number != want && e.Number != "") {
			continue
		}
		rank := 100
		switch {
		case e.Family == x11.FamilyInternet && ip.To4() != nil && bytes.Equal([]byte(e.Address), ip.To4()):
			rank = 0
		case e.Family == 6 && ip != nil && ip.To4() == nil && bytes.Equal([]byte(e.Address), ip.To16()): // FamilyInternet6
			rank = 0
		case e.Family == x11.FamilyLocal && local && e.Address == hostname:
			rank = 2
		case e.Family == x11.FamilyWild:
			rank = 4
		}
		if e.Number == "" {
			rank++
		}
		if rank < best {
			best, cookie = rank, e.Data
		}
	}
	if best == 100 {
		return "", nil
	}
	return x11.MITCookie, cookie
}

package wine

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// pingRangePath holds the groups that may open unprivileged ICMP sockets.
// Wine's IcmpSendEcho (the menu ping after patches #2 and #3) needs one.
const pingRangePath = "/proc/sys/net/ipv4/ping_group_range"

// PingAllowed says whether this user may send ICMP echo without privileges:
// its primary group or one of its groups is in net.ipv4.ping_group_range.
func PingAllowed() (bool, error) {
	data, err := os.ReadFile(pingRangePath)
	if err != nil {
		return false, err
	}
	lo, hi, err := parsePingRange(string(data))
	if err != nil {
		return false, err
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false, err
	}
	for _, g := range append(groups, os.Getgid()) {
		if int64(g) >= lo && int64(g) <= hi {
			return true, nil
		}
	}
	return false, nil
}

// parsePingRange reads "<low> <high>". The kernel's default "1 0" (low above
// high) allows nobody.
func parsePingRange(s string) (lo, hi int64, err error) {
	f := strings.Fields(s)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("odd ping_group_range %q", s)
	}
	if lo, err = strconv.ParseInt(f[0], 10, 64); err != nil {
		return 0, 0, err
	}
	if hi, err = strconv.ParseInt(f[1], 10, 64); err != nil {
		return 0, 0, err
	}
	return lo, hi, nil
}

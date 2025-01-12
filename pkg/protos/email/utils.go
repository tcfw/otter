package email

import (
	"errors"
	"strings"
)

func SplitAddrUserDomain(addr string) (string, string, error) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return "", "", errors.New("no at symbol in address")
	}

	local, domain := addr[:at], addr[at+1:]
	return local, domain, nil
}

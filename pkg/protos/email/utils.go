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

	localParts := strings.SplitN(local, "+", 2)

	return localParts[0], domain, nil
}

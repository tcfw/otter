package version

import (
	"fmt"
	"time"
)

var (
	version    = "0.0.0"
	commitHash = "unspecified"
	buildTime  = time.Now().String()
)

func Version() string {
	return version
}

func BuildTime() string {
	return buildTime
}

func FullVersion() string {
	return fmt.Sprintf("%s-%s", version, commitHash)
}

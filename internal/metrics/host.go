package metrics

import (
	"runtime"

	"github.com/shirou/gopsutil/cpu"
)

func getOSType() string {
	return runtime.GOOS
}

func getMachineArch() string {
	return runtime.GOARCH
}

func getCPUCount() (int, error) {
	info, err := cpu.Info()
	if err != nil {
		return 0, err
	}

	count := 0

	for _, coreInfo := range info {
		count += int(coreInfo.Cores)
	}

	return len(info), nil
}

func getCPUInfo() (string, error) {
	info, err := cpu.Info()
	if err != nil {
		return "", err
	}

	if len(info) == 0 {
		return "", nil
	}

	return info[0].ModelName, nil
}

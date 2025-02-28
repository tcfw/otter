package metrics

import (
	"github.com/tcfw/otter/pkg/config"

	"github.com/shirou/gopsutil/disk"
)

func getTotalDiskSpace() (uint64, error) {
	dir := config.GetConfigAs("", config.Storage_Dir).(string)

	us, err := disk.Usage(dir)
	if err != nil {
		return 0, err
	}

	return us.Total, nil
}

func getAvailableDiskSpace() (uint64, error) {
	dir := config.GetConfigAs("", config.Storage_Dir).(string)

	us, err := disk.Usage(dir)
	if err != nil {
		return 0, err
	}

	return us.Free, nil

}

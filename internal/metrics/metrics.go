package metrics

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tcfw/otter/internal/metrics/pb"
	"github.com/tcfw/otter/internal/version"
	"github.com/tcfw/otter/pkg/plugins"

	protobuf "google.golang.org/protobuf/proto"
)

type SetKey string

const (
	SetKeyDiskTotal     SetKey = "disk.total_bytes"
	SetKeyDiskAvail     SetKey = "disk.available_bytes"
	SetKeyHostOS        SetKey = "host.os"
	SetKeyHostArch      SetKey = "host.arch"
	SetKeyHostCoreCount SetKey = "host.core_count"
	SetKeyHostCPUInfo   SetKey = "host.cpu"
	SetKeyVersion       SetKey = "otter.version"
	SetKeyPlugins       SetKey = "otter.plugins"
	SetKeyHostID        SetKey = "otter.host_id"
)

type Set map[SetKey]any

func (s Set) Marshal() ([]byte, error) {
	pbs := &pb.Set{Collection: map[string]*pb.SetType{}}

	for k, v := range s {
		mv := &pb.SetType{}

		switch vt := v.(type) {
		case string:
			mv.Value = &pb.SetType_StringValue{StringValue: vt}
		case int64:
			mv.Value = &pb.SetType_IntValue{IntValue: vt}
		case int:
			mv.Value = &pb.SetType_IntValue{IntValue: int64(vt)}
		case int32:
			mv.Value = &pb.SetType_IntValue{IntValue: int64(vt)}
		case uint64:
			mv.Value = &pb.SetType_IntValue{IntValue: int64(vt)}
		case uint32:
			mv.Value = &pb.SetType_IntValue{IntValue: int64(vt)}
		default:
			return nil, fmt.Errorf("unsupported metrics type: %T", v)
		}

		pbs.Collection[string(k)] = mv
	}

	return protobuf.Marshal(pbs)
}

func (s Set) Unmarshal(b []byte) error {
	pbs := &pb.Set{}

	if err := protobuf.Unmarshal(b, pbs); err != nil {
		return err
	}

	for k, v := range pbs.Collection {
		switch vt := v.Value.(type) {
		case *pb.SetType_StringValue:
			s[SetKey(k)] = vt.StringValue
		case *pb.SetType_IntValue:
			s[SetKey(k)] = vt.IntValue
		default:
			return fmt.Errorf("unsupported metrics type for key '%s'", k)
		}
	}

	return nil
}

func Collect() (Set, error) {
	set := make(Set)
	errs := []error{}

	//disk
	total, err := getTotalDiskSpace()
	errs = append(errs, err)
	set[SetKeyDiskTotal] = total

	avail, err := getAvailableDiskSpace()
	errs = append(errs, err)
	set[SetKeyDiskAvail] = avail

	//host
	set[SetKeyHostOS] = getOSType()
	set[SetKeyHostArch] = getMachineArch()

	count, err := getCPUCount()
	errs = append(errs, err)
	set[SetKeyHostCoreCount] = count

	cpu, err := getCPUInfo()
	errs = append(errs, err)
	set[SetKeyHostCPUInfo] = cpu

	//otter
	set[SetKeyVersion] = version.FullVersion()

	pluginNames := []string{}
	for _, plugin := range plugins.LoadedPlugins() {
		pluginNames = append(pluginNames, plugin.Name())
	}
	set[SetKeyPlugins] = strings.Join(pluginNames, ",")

	return set, errors.Join(errs...)
}

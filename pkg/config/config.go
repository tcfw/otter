package config

import "github.com/spf13/viper"

type ConfigKey string

const (
	Storage_Dir       ConfigKey = "storage.dir"
	Storage_SealedDEK ConfigKey = "storage.sealedDEK"

	API_ListenAddrs ConfigKey = "api.listenAddrs"

	P2P_ListenAddrs    ConfigKey = "p2p.listenAddrs"
	P2P_HostKey        ConfigKey = "p2p.hostKey"
	P2P_BootstrapAddrs ConfigKey = "p2p.bootstrapAddrs"
	P2P_NAT            ConfigKey = "p2p.nat"

	Plugins_LoadDir ConfigKey = "plugins.loadDir"

	POIS_ListenAddrs    ConfigKey = "pois.listenAddrs"
	POIS_EnableTLS      ConfigKey = "pois.enableAutoTLS"
	POIS_TLSCertificate ConfigKey = "pois.tlsCertificate"
	POIS_TLSKey         ConfigKey = "pois.tlsKey"
)

func GetConfig(k ConfigKey) any {
	return viper.Get(string(k))
}

func GetConfigAs(t any, k ConfigKey) any {
	switch t.(type) {
	case []string:
		return viper.GetStringSlice(string(k))
	case string:
		return viper.GetString(string(k))
	case bool:
		return viper.GetBool(string(k))
	default:
		return nil
	}
}

func SetConfig(k ConfigKey, v any) {
	viper.Set(string(k), v)
}

func SetAndStoreConfig(k ConfigKey, v any) error {
	viper.Set(string(k), v)
	return viper.WriteConfig()
}

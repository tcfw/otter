package config

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
)

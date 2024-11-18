package config

type ConfigKey string

const (
	P2P_ListenAddrs    ConfigKey = "p2p.listenAddrs"
	P2P_HostKey        ConfigKey = "p2p.hostKey"
	P2P_BootstrapAddrs ConfigKey = "p2p.bootstrapAddrs"

	Plugins_LoadDir ConfigKey = "plugins.loadDir"
)

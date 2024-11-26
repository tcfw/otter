package internal

import (
	"errors"

	"github.com/spf13/viper"
	"github.com/tcfw/otter/pkg/config"
)

var (
	defaultConfig = map[config.ConfigKey]any{
		config.P2P_ListenAddrs: []string{
			"/ip4/0.0.0.0/tcp/9696",
			"/ip6/::/tcp/9696",
			"/ip4/0.0.0.0/udp/9696/webrtc-direct",
			"/ip4/0.0.0.0/udp/9696/quic-v1",
			"/ip4/0.0.0.0/udp/9696/quic-v1/webtransport",
			"/ip6/::/udp/9696/webrtc-direct",
			"/ip6/::/udp/9696/quic-v1",
			"/ip6/::/udp/9696/quic-v1/webtransport",
		},
		config.Plugins_LoadDir: "./plugins",
		config.Storage_Dir:     "./data",
		config.P2P_NAT:         true,
	}

	errUnsupportedSettingType = errors.New("unsupported config value type")
)

func init() {
	viper.SetConfigName("otter")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.otter")
	viper.AutomaticEnv()

	setDefaultConfig()
}

func setDefaultConfig() {
	for k, v := range defaultConfig {
		viper.SetDefault(string(k), v)
	}
}

func (o *Otter) GetConfig(k config.ConfigKey) any {
	return viper.Get(string(k))
}

func (o *Otter) GetConfigAs(t any, k config.ConfigKey) any {
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

func (o *Otter) SetConfig(k config.ConfigKey, v any) {
	viper.Set(string(k), v)
}

func (o *Otter) SetAndStoreConfig(k config.ConfigKey, v any) error {
	viper.Set(string(k), v)
	return viper.WriteConfig()
}

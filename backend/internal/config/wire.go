package config

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/google/wire"
)

// ProviderSet 提供配置层的依赖
var ProviderSet = wire.NewSet(
	ProvideConfig,
)

// ProvideConfig 提供应用配置
func ProvideConfig() (*Config, error) {
	cfg, err := LoadForBootstrap()
	if err != nil {
		return nil, err
	}
	if err := outbound.Configure(cfg.Security.Outbound.AllowPrivateTargets); err != nil {
		return nil, err
	}
	return cfg, nil
}

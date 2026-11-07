package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-gost/x/identity"
)

// Config 配置结构体
type Config struct {
	Addr   string `json:"addr"`
	Secret string `json:"secret"`
	Http   int    `json:"http"`
	Tls    int    `json:"tls"`
	Socks  int    `json:"socks"`
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	// 读取文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 解析JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 验证必要的配置项
	if config.Addr == "" {
		return nil, fmt.Errorf("服务器地址不能为空")
	}

	return &config, nil
}

// ApplyEnvConfig writes FLYX_AGENT_ADDR and FLYX_AGENT_SECRET (with legacy aliases) into the
// config file, creating it when missing. The Docker image is configured this
// way; the ports the panel stored in the file are kept.
func ApplyEnvConfig(configPath string) error {
	addr := identity.Env("FLYX_AGENT_ADDR", "FLUX_AGENT_ADDR")
	secret := identity.Env("FLYX_AGENT_SECRET", "FLUX_AGENT_SECRET")
	if addr == "" && secret == "" {
		return nil
	}

	var config Config
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("解析配置文件失败: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}
	if addr != "" {
		config.Addr = addr
	}
	if secret != "" {
		config.Secret = secret
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	APIBase    string `mapstructure:"api_base"`
	AdminKey   string `mapstructure:"admin_key"`
	IssuedBy   string `mapstructure:"issued_by"`
	ProductVer string `mapstructure:"product_version"`
}

var cfg Config

func loadConfig() error {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		cfgDir = os.Getenv("HOME")
	}
	dir := filepath.Join(cfgDir, "logistics-license-manager")

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(dir)
	viper.AddConfigPath(".")

	viper.SetDefault("api_base",        "https://license.yourcompany.com")
	viper.SetDefault("issued_by",       "admin")
	viper.SetDefault("product_version", "4.0.0")

	// Allow env override: LM_API_BASE, LM_ADMIN_KEY, etc.
	viper.SetEnvPrefix("LM")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("config read error: %w", err)
		}
		// First run — create default config
		if err2 := os.MkdirAll(dir, 0o700); err2 != nil {
			return fmt.Errorf("cannot create config dir: %w", err2)
		}
		viper.WriteConfigAs(filepath.Join(dir, "config.toml"))
	}

	return viper.Unmarshal(&cfg)
}

func saveConfig() {
	viper.Set("api_base",        cfg.APIBase)
	viper.Set("admin_key",       cfg.AdminKey)
	viper.Set("issued_by",       cfg.IssuedBy)
	viper.Set("product_version", cfg.ProductVer)
	_ = viper.WriteConfig()
}

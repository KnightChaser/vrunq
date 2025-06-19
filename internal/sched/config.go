package sched

import (
	"os"

	yaml "github.com/goccy/go-yaml"
)

// config mirros config.yaml
type Config struct {
	TickMS     int `yaml:"tick_ms"`     // 5 (by default)
	SliceTicks int `yaml:"slice_ticks"` // 5 (by default)
}

// If the config file is not found, we use default values
func defaultConfig() Config {
	return Config{
		TickMS:     5,
		SliceTicks: 5,
	}
}

// Load reads YAML and overrides defaults; empty path = defautls only
func Load(path string) Config {
	cfg := defaultConfig()

	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	_ = yaml.Unmarshal(data, &cfg)

	// sanity clamps
	if cfg.SliceTicks <= 0 {
		cfg.SliceTicks = 5
	}
	if cfg.TickMS <= 0 {
		cfg.TickMS = 5
	}

	return cfg
}

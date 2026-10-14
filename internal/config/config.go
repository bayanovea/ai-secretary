// Package config loads the secretary's configuration from a YAML file.
//
// Every field has a default, so a missing file or a partially filled one is not
// an error: Load fills in whatever the file leaves out. Only unreadable or
// syntactically invalid YAML fails.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Tier bounds. Tier 1 is critical, tier 3 is background.
const (
	MinTier = 1
	MaxTier = 3
)

// Defaults applied when the config file or an individual field is absent.
const (
	DefaultPersona        = "alfred"
	DefaultArtifact       = "docs/duties.md"
	DefaultArchive        = "docs/duties-archive.md"
	DefaultSoonWindowDays = 3
	DefaultStaleDays      = 14
)

// TierConfig describes how much slack a tier gets before its latest start date.
//
// The buffer is max(BufferMinDays, BufferRatio * effort), so a tier-1 task that
// takes ten days gets five days of buffer while a one-hour one still gets two.
type TierConfig struct {
	Name          string  `yaml:"name"`
	BufferMinDays int     `yaml:"buffer_min_days"`
	BufferRatio   float64 `yaml:"buffer_ratio"`
}

// Config is the whole of secretary.yml.
type Config struct {
	Persona  string   `yaml:"persona"`
	Artifact string   `yaml:"artifact"`
	Archive  string   `yaml:"archive"`
	Sections []string `yaml:"sections"`
	Contexts []string `yaml:"contexts"`
	// Tiers is keyed by the tier number as a string ("1", "2", "3") so the YAML
	// stays readable.
	Tiers          map[string]TierConfig `yaml:"tiers"`
	SoonWindowDays int                   `yaml:"soon_window_days"`
	StaleDays      int                   `yaml:"stale_days"`
}

// Default returns the configuration used when nothing is specified.
func Default() *Config {
	return &Config{
		Persona:  DefaultPersona,
		Artifact: DefaultArtifact,
		Archive:  DefaultArchive,
		Sections: []string{"Work", "Personal", "Home", "Health", "Relationships", "Finance"},
		Contexts: []string{"@town", "@home", "@phone", "@computer", "@work"},
		Tiers: map[string]TierConfig{
			"1": {Name: "Critical", BufferMinDays: 2, BufferRatio: 0.5},
			"2": {Name: "Important", BufferMinDays: 1, BufferRatio: 0.25},
			"3": {Name: "Background", BufferMinDays: 0, BufferRatio: 0},
		},
		SoonWindowDays: DefaultSoonWindowDays,
		StaleDays:      DefaultStaleDays,
	}
}

// Load reads the config at path. A missing file yields Default(); a present one
// has its absent fields filled from Default(). Malformed YAML is an error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// applyDefaults fills every field the file left out. A tier that is present in
// the file is taken as written — only its name falls back — so an explicit zero
// buffer stays zero.
func (c *Config) applyDefaults() {
	def := Default()

	if c.Persona == "" {
		c.Persona = def.Persona
	}
	if c.Artifact == "" {
		c.Artifact = def.Artifact
	}
	if c.Archive == "" {
		c.Archive = def.Archive
	}
	if len(c.Sections) == 0 {
		c.Sections = def.Sections
	}
	if len(c.Contexts) == 0 {
		c.Contexts = def.Contexts
	}
	if c.SoonWindowDays == 0 {
		c.SoonWindowDays = def.SoonWindowDays
	}
	if c.StaleDays == 0 {
		c.StaleDays = def.StaleDays
	}

	if c.Tiers == nil {
		c.Tiers = make(map[string]TierConfig, len(def.Tiers))
	}
	for key, defTier := range def.Tiers {
		tier, ok := c.Tiers[key]
		if !ok {
			c.Tiers[key] = defTier
			continue
		}
		if tier.Name == "" {
			tier.Name = defTier.Name
			c.Tiers[key] = tier
		}
	}
}

// Tier returns the configuration for tier n. An out-of-range or unconfigured
// tier yields the zero TierConfig, whose empty Name marks it as invalid.
func (c *Config) Tier(n int) TierConfig {
	if n < MinTier || n > MaxTier {
		return TierConfig{}
	}
	return c.Tiers[fmt.Sprint(n)]
}

// HasSection reports whether name is one of the configured sections.
func (c *Config) HasSection(name string) bool {
	for _, s := range c.Sections {
		if s == name {
			return true
		}
	}
	return false
}

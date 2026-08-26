package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// write puts content in a temp file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

const fullYAML = `
persona: alfred
artifact: docs/duties.md
archive: docs/duties-archive.md
sections: [Работа, Личное, Дом, Здоровье, Отношения, Финансы]
contexts: ["@город", "@дом", "@телефон", "@компьютер", "@работа"]
tiers:
  "1": {name: Критично, buffer_min_days: 2, buffer_ratio: 0.5}
  "2": {name: Важно,    buffer_min_days: 1, buffer_ratio: 0.25}
  "3": {name: Фоновое,  buffer_min_days: 0, buffer_ratio: 0}
soon_window_days: 3
stale_days: 14
`

func TestLoad(t *testing.T) {
	def := Default()

	tests := []struct {
		name    string
		yaml    string // written to a file unless missing is set
		missing bool
		wantErr bool
		check   func(t *testing.T, c *Config)
	}{
		{
			name: "full file",
			yaml: fullYAML,
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c, def) {
					t.Errorf("full file did not match Default():\n got %+v\nwant %+v", c, def)
				}
			},
		},
		{
			name: "partial file fills defaults",
			yaml: "persona: alfred\nartifact: my/duties.md\n",
			check: func(t *testing.T, c *Config) {
				if c.Artifact != "my/duties.md" {
					t.Errorf("Artifact = %q, want my/duties.md", c.Artifact)
				}
				if c.Archive != DefaultArchive {
					t.Errorf("Archive = %q, want default %q", c.Archive, DefaultArchive)
				}
				if c.SoonWindowDays != DefaultSoonWindowDays {
					t.Errorf("SoonWindowDays = %d, want %d", c.SoonWindowDays, DefaultSoonWindowDays)
				}
				if c.StaleDays != DefaultStaleDays {
					t.Errorf("StaleDays = %d, want %d", c.StaleDays, DefaultStaleDays)
				}
				if !reflect.DeepEqual(c.Sections, def.Sections) {
					t.Errorf("Sections = %v, want defaults %v", c.Sections, def.Sections)
				}
				if !reflect.DeepEqual(c.Contexts, def.Contexts) {
					t.Errorf("Contexts = %v, want defaults %v", c.Contexts, def.Contexts)
				}
				if !reflect.DeepEqual(c.Tiers, def.Tiers) {
					t.Errorf("Tiers = %v, want defaults %v", c.Tiers, def.Tiers)
				}
			},
		},
		{
			name: "empty file is all defaults",
			yaml: "",
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c, def) {
					t.Errorf("empty file did not match Default():\n got %+v\nwant %+v", c, def)
				}
			},
		},
		{
			name:    "missing file is all defaults",
			missing: true,
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c, def) {
					t.Errorf("missing file did not match Default():\n got %+v\nwant %+v", c, def)
				}
			},
		},
		{
			name: "partial tiers keep explicit values and fill the rest",
			yaml: "tiers:\n  \"2\": {buffer_min_days: 7, buffer_ratio: 0.9}\n",
			check: func(t *testing.T, c *Config) {
				got := c.Tier(2)
				want := TierConfig{Name: "Важно", BufferMinDays: 7, BufferRatio: 0.9}
				if got != want {
					t.Errorf("Tier(2) = %+v, want %+v", got, want)
				}
				if c.Tier(1) != def.Tier(1) {
					t.Errorf("Tier(1) = %+v, want default %+v", c.Tier(1), def.Tier(1))
				}
				if c.Tier(3) != def.Tier(3) {
					t.Errorf("Tier(3) = %+v, want default %+v", c.Tier(3), def.Tier(3))
				}
			},
		},
		{
			name: "explicit zero buffer survives",
			yaml: "tiers:\n  \"1\": {name: Критично, buffer_min_days: 0, buffer_ratio: 0}\n",
			check: func(t *testing.T, c *Config) {
				got := c.Tier(1)
				want := TierConfig{Name: "Критично", BufferMinDays: 0, BufferRatio: 0}
				if got != want {
					t.Errorf("Tier(1) = %+v, want %+v", got, want)
				}
			},
		},
		{
			name: "custom sections replace defaults entirely",
			yaml: "sections: [Гараж]\n",
			check: func(t *testing.T, c *Config) {
				if !reflect.DeepEqual(c.Sections, []string{"Гараж"}) {
					t.Errorf("Sections = %v, want [Гараж]", c.Sections)
				}
			},
		},
		{
			name:    "malformed yaml",
			yaml:    "persona: [unclosed\n",
			wantErr: true,
		},
		{
			name:    "wrong type for a field",
			yaml:    "soon_window_days: не число\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "absent.yml")
			if !tt.missing {
				path = write(t, "secretary.yml", tt.yaml)
			}

			got, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() error = nil, want an error")
				}
				if got != nil {
					t.Errorf("Load() = %+v on error, want nil", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got == nil {
				t.Fatal("Load() = nil with no error")
			}
			tt.check(t, got)
		})
	}
}

// The checked-in secretary.yml must load and match the built-in defaults,
// otherwise the documented format and the code have drifted apart.
func TestLoadRepoConfig(t *testing.T) {
	got, err := Load(filepath.Join("..", "..", "secretary.yml"))
	if err != nil {
		t.Fatalf("Load(secretary.yml) error = %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("secretary.yml does not match Default():\n got %+v\nwant %+v", got, Default())
	}
}

func TestLoadUnreadable(t *testing.T) {
	// A directory is readable as a path but not as a file.
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("Load(dir) error = nil, want an error")
	}
}

func TestTier(t *testing.T) {
	cfg := Default()

	tests := []struct {
		name string
		n    int
		want TierConfig
	}{
		{"tier 1", 1, TierConfig{Name: "Критично", BufferMinDays: 2, BufferRatio: 0.5}},
		{"tier 2", 2, TierConfig{Name: "Важно", BufferMinDays: 1, BufferRatio: 0.25}},
		{"tier 3", 3, TierConfig{Name: "Фоновое", BufferMinDays: 0, BufferRatio: 0}},
		{"zero", 0, TierConfig{}},
		{"negative", -1, TierConfig{}},
		{"above range", 4, TierConfig{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.Tier(tt.n); got != tt.want {
				t.Errorf("Tier(%d) = %+v, want %+v", tt.n, got, tt.want)
			}
		})
	}
}

func TestTierUnconfigured(t *testing.T) {
	cfg := &Config{Tiers: map[string]TierConfig{}}
	if got := cfg.Tier(2); got != (TierConfig{}) {
		t.Errorf("Tier(2) on an empty map = %+v, want zero", got)
	}
}

func TestHasSection(t *testing.T) {
	cfg := Default()

	tests := []struct {
		name    string
		section string
		want    bool
	}{
		{"first section", "Работа", true},
		{"last section", "Финансы", true},
		{"unknown section", "Гараж", false},
		{"empty string", "", false},
		{"case sensitive", "работа", false},
		{"whitespace not trimmed", " Работа", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.HasSection(tt.section); got != tt.want {
				t.Errorf("HasSection(%q) = %v, want %v", tt.section, got, tt.want)
			}
		})
	}
}

func TestHasSectionNoSections(t *testing.T) {
	cfg := &Config{}
	if cfg.HasSection("Работа") {
		t.Error("HasSection on a config with no sections = true, want false")
	}
}

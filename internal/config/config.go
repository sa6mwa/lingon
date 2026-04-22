package config

import (
	"errors"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration for Lingon.
type Config struct {
	Server   ServerConfig   `mapstructure:"server" yaml:"server"`
	Client   ClientConfig   `mapstructure:"client" yaml:"client"`
	Terminal TerminalConfig `mapstructure:"terminal" yaml:"terminal"`
}

// ServerConfig configures the relay/server mode.
type ServerConfig struct {
	Listen             string          `mapstructure:"listen" yaml:"listen"`
	DataDir            string          `mapstructure:"data_dir" yaml:"data_dir"`
	UsersFile          string          `mapstructure:"users_file" yaml:"users_file"`
	BasePath           string          `mapstructure:"base" yaml:"base"`
	ReplayHistoryBytes int             `mapstructure:"replay_history_bytes" yaml:"replay_history_bytes"`
	TLS                TLSConfig       `mapstructure:"tls" yaml:"tls"`
	WebUI              WebUIConfig     `mapstructure:"webui" yaml:"webui"`
	Wall               WallConfig      `mapstructure:"wall" yaml:"wall"`
	ConnectLimit       ConnectLimitCfg `mapstructure:"connect_limit" yaml:"connect_limit"`
}

// WebUIConfig configures web UI behavior.
type WebUIConfig struct {
	NoBanner bool `mapstructure:"no_banner" yaml:"no_banner"`
}

// WallConfig configures relay wall notifications.
type WallConfig struct {
	Timeout       time.Duration `mapstructure:"timeout" yaml:"timeout"`
	InactiveAfter string        `mapstructure:"inactive_after" yaml:"inactive_after"`
}

// ClientConfig configures client defaults.
type ClientConfig struct {
	Endpoint string `mapstructure:"endpoint" yaml:"endpoint"`
	AuthFile string `mapstructure:"auth_file" yaml:"auth_file"`
	LogFile  string `mapstructure:"log_file" yaml:"log_file"`
	// LoginNonInteractive forces non-interactive login mode for `lingon login`.
	LoginNonInteractive bool `mapstructure:"login_non_interactive" yaml:"login_non_interactive"`
}

// TerminalConfig configures local terminal emulation defaults.
type TerminalConfig struct {
	Term                        string `mapstructure:"term" yaml:"term"`
	Respawn                     bool   `mapstructure:"respawn" yaml:"respawn"`
	ScrollbackLines             int    `mapstructure:"scrollback_lines" yaml:"scrollback_lines"`
	Theme                       string `mapstructure:"theme" yaml:"theme"`
	HostnameOnly                bool   `mapstructure:"hostname_only" yaml:"hostname_only"`
	DisableDesktopNotifications bool   `mapstructure:"disable_desktop_notifications" yaml:"disable_desktop_notifications"`
	WallInactiveAfter           string `mapstructure:"wall_inactive_after" yaml:"wall_inactive_after"`
}

// TLSConfig configures TLS behavior for the relay/server.
type TLSConfig struct {
	Mode     string   `mapstructure:"mode" yaml:"mode"`
	Bundle   []string `mapstructure:"bundle" yaml:"bundle"`
	Hostname string   `mapstructure:"hostname" yaml:"hostname"`
	Dir      string   `mapstructure:"dir" yaml:"dir"`
	CacheDir string   `mapstructure:"cache_dir" yaml:"cache_dir"`
}

// ConnectLimitCfg configures the global connection limiter.
type ConnectLimitCfg struct {
	Disable bool          `mapstructure:"disable" yaml:"disable"`
	Burst   int           `mapstructure:"burst" yaml:"burst"`
	Count   int           `mapstructure:"count" yaml:"count"`
	Window  time.Duration `mapstructure:"window" yaml:"window"`
}

// Loader wraps Viper configuration loading for Lingon.
type Loader struct {
	v          *viper.Viper
	configFile string
}

// NewLoader initializes a Loader with standard defaults.
func NewLoader() *Loader {
	v := viper.New()
	v.SetEnvPrefix("LINGON")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	v.SetConfigName("config")
	v.AddConfigPath(".")
	v.AddConfigPath(DefaultConfigDir())

	return &Loader{v: v}
}

// Viper exposes the underlying Viper instance for flag binding and defaults.
func (l *Loader) Viper() *viper.Viper {
	return l.v
}

// ConfigFileUsed reports the resolved config path used by Viper, if any.
func (l *Loader) ConfigFileUsed() string {
	return l.v.ConfigFileUsed()
}

// SetConfigFile sets an explicit config file path.
func (l *Loader) SetConfigFile(path string) {
	l.configFile = strings.TrimSpace(path)
}

// ReadInConfig reads configuration from file if available.
func (l *Loader) ReadInConfig() error {
	if l.configFile != "" {
		l.v.SetConfigFile(l.configFile)
	}

	if err := l.v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	return nil
}

// Load reads configuration and unmarshals it into a Config struct.
func (l *Loader) Load() (Config, error) {
	if err := l.ReadInConfig(); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	resolveConfigPaths(&cfg, l.v.ConfigFileUsed())
	return cfg, nil
}

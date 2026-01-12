package config

import (
	"errors"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration for Lingon.
type Config struct {
	Server ServerConfig `mapstructure:"server" yaml:"server"`
	Client ClientConfig `mapstructure:"client" yaml:"client"`
	Terminal TerminalConfig `mapstructure:"terminal" yaml:"terminal"`
}

// ServerConfig configures the relay/server mode.
type ServerConfig struct {
	Listen    string    `mapstructure:"listen" yaml:"listen"`
	DataDir   string    `mapstructure:"data_dir" yaml:"data_dir"`
	UsersFile string    `mapstructure:"users_file" yaml:"users_file"`
	BasePath  string    `mapstructure:"base" yaml:"base"`
	TLS       TLSConfig `mapstructure:"tls" yaml:"tls"`
}

// ClientConfig configures client defaults.
type ClientConfig struct {
	Endpoint    string `mapstructure:"endpoint" yaml:"endpoint"`
	AuthFile    string `mapstructure:"auth_file" yaml:"auth_file"`
	LogFile     string `mapstructure:"log_file" yaml:"log_file"`
	BufferLines int    `mapstructure:"buffer_lines" yaml:"buffer_lines"`
}

// TerminalConfig configures local terminal emulation defaults.
type TerminalConfig struct {
	Term string `mapstructure:"term" yaml:"term"`
}

// TLSConfig configures TLS behavior for the relay/server.
type TLSConfig struct {
	Mode     string   `mapstructure:"mode" yaml:"mode"`
	Bundle   []string `mapstructure:"bundle" yaml:"bundle"`
	Hostname string   `mapstructure:"hostname" yaml:"hostname"`
	Dir      string   `mapstructure:"dir" yaml:"dir"`
	CacheDir string   `mapstructure:"cache_dir" yaml:"cache_dir"`
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
	v.AddConfigPath("$HOME/.config/lingon")
	v.AddConfigPath("$HOME/.lingon")

	return &Loader{v: v}
}

// Viper exposes the underlying Viper instance for flag binding and defaults.
func (l *Loader) Viper() *viper.Viper {
	return l.v
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
	return cfg, nil
}

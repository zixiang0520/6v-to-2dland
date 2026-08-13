package cfg

import (
	"encoding/json"
	"fmt"
	"os"
)

const DefaultKDocsURL = "https://www.kdocs.cn/l/cmMxolBHuN1p"

type Config struct {
	Listen         string `json:"listen"`
	AccessPassword string `json:"access_password"`
	KDocsURL       string `json:"kdocs_url"`
}

func Defaults() *Config {
	return &Config{
		Listen:   ":8080",
		KDocsURL: DefaultKDocsURL,
	}
}

func Load(path string) (*Config, error) {
	c := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.KDocsURL == "" {
		c.KDocsURL = DefaultKDocsURL
	}
	return c, nil
}

func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

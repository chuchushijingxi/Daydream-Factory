// Package config supplies explicit, public deployment settings to free tests.
package config

import (
	"os"
	"path/filepath"
	"runtime"

	"drama-agent/internal/adapter/appconfig"
	"gopkg.in/yaml.v3"
)

func Example() appconfig.Config {
	_, here, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../../config/app.example.yaml"))
	if err != nil {
		panic(err)
	}
	cfg := appconfig.Defaults()
	if err = yaml.Unmarshal(raw, &cfg); err != nil {
		panic(err)
	}
	if err = cfg.Validate(); err != nil {
		panic(err)
	}
	return cfg
}

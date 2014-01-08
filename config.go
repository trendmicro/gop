package gop

import (
	"github.com/vaughan0/go-ini"

	"fmt"
	"os"
	"strconv"
	"strings"
)

type ConfigSource interface {
	Get(sName, k string, def string) (string, bool)
}

type Config struct {
	source		ConfigSource
	overrides 	map[string]map[string]string
}

type Section map[string]string
type ConfigMap map[string]Section


func (a *App) loadAppConfigFile() {
	// We do not have logging set up yet. We just panic() on error.

	rootEnvName := strings.ToUpper(a.ProjectName) + "_CFG_ROOT"
	configRoot := os.Getenv(rootEnvName)
	if configRoot == "" {
		configRoot = "/etc/" + a.ProjectName
	}

	fileEnvName := strings.ToUpper(a.ProjectName) + "_" + strings.ToUpper(a.AppName) + "_CFG_FILE"
	configFname := os.Getenv(fileEnvName)
	if configFname == "" {
		configFname = configRoot + "/" + a.AppName + ".conf"
	}

	cfg, err := ini.LoadFile(configFname)
	if err != nil {
		// Can't log, it's all too early
		panic(fmt.Sprintf("Can't load config file [%s]: %s", configFname, err.Error()))
	}

	configSource := make(ConfigMap)
	for section, m := range cfg {
		configSource[section] = make(map[string]string)
		for k, v := range m {
			configSource[section][k] = v
		}
	}
	a.Cfg = Config{
		source:		&configSource, 
		overrides:	make(map[string]map[string]string),
	}
}

func (cfg *ConfigMap) Get(sName, k string, def string) (string, bool) {
	s, ok := map[string]Section(*cfg)[sName]
	if !ok {
		return def, false
	}
	v, ok := map[string]string(s)[k]
	if !ok {
		return def, false
	}
	return v, true
}




func (cfg *Config) Override(sectionName, key, val string) {
	section, ok := cfg.overrides[sectionName]
	if !ok {
		cfg.overrides[sectionName] = make(map[string]string)
		section = cfg.overrides[sectionName]
	}
	section[key] = val
}

func (cfg *Config) Get(sectionName, key string, def string) (string, bool) {
	section, ok := cfg.overrides[sectionName]
	if ok {
		// Ooh...we have a section
		val, ok := section[key]
		if ok {
			// Oh! and a key. Lets have that then
			return val, true
		}
	}
	// Not found, just punt it to the base
	return cfg.source.Get(sectionName, key, def)
}


func (cfg *Config) GetInt(sName, k string, def int) (int, bool) {
	v, found := cfg.source.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.Atoi(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", k, v, err))
}
func (cfg *Config) GetInt64(sName, k string, def int64) (int64, bool) {
	v, found := cfg.source.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseInt(v, 10, 64)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", k, v, err))
}
func (cfg *Config) GetBool(sName, k string, def bool) (bool, bool) {
	v, found := cfg.source.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseBool(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Bad boolean config key %s: %s", k, v))
}
func (cfg *Config) GetFloat32(sName, k string, def float32) (float32, bool) {
	v, found := cfg.source.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseFloat(v, 32)
	if err == nil {
		return float32(r), true
	}
	panic(fmt.Sprintf("Non-numeric float32 config key %s: %s [%s]", k, v, err))
}
func (cfg *Config) GetList(sName, k string, def []string) ([]string, bool) {
	vStr, found := cfg.source.Get(sName, k, "")
	if !found {
		return def, false
	}
	v := strings.Split(vStr, ",")
	for i := 0; i < len(v); i++ {
		v[i] = strings.TrimSpace(v[i])
	}
	return v, true
}

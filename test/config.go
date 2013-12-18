package test

import (
	"fmt"
	"github.com/trendmicro/gop"
	"strconv"
)

type TestConfig struct {
	base      gop.Config
	overrides map[string]map[string]string
}

func NewConfig(base gop.Config) *TestConfig {
	return &TestConfig{
		base:      base,
		overrides: make(map[string]map[string]string),
	}
}

func (cfg *TestConfig) Override(sectionName, key, val string) {
	section, ok := cfg.overrides[sectionName]
	if !ok {
		cfg.overrides[sectionName] = make(map[string]string)
		section = cfg.overrides[sectionName]
	}
	section[key] = val
}

func (cfg *TestConfig) Get(sectionName, key string, def string) (string, bool) {
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
	return cfg.base.Get(sectionName, key, def)
}

func (cfg *TestConfig) GetInt(sName, k string, def int) (int, bool) {
	v, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.Atoi(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", k, v, err))
}
func (cfg *TestConfig) GetInt64(sName, k string, def int64) (int64, bool) {
	v, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseInt(v, 10, 64)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", k, v, err))
}
func (cfg *TestConfig) GetBool(sName, k string, def bool) (bool, bool) {
	v, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseBool(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Bad boolean config key %s: %s", k, v))
}
func (cfg *TestConfig) GetFloat32(sName, k string, def float32) (float32, bool) {
	v, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	r, err := strconv.ParseFloat(v, 32)
	if err == nil {
		return float32(r), true
	}
	panic(fmt.Sprintf("Non-numeric float32 config key %s: %s [%s]", k, v, err))
}

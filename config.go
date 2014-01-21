package gop

import (
	"encoding/json"
	"github.com/vaughan0/go-ini"
	"io/ioutil"
	"log"
	"time"

	"fmt"
	"os"
	"strconv"
	"strings"
)

type ConfigSource interface {
	Get(sName, k string, def string) (string, bool)
	Add(sName, k, v string)
	Sections() []string
	SectionKeys(sName string) []string
}

type Config struct {
	source              ConfigMap
	persistentOverrides ConfigMap
	transientOverrides  ConfigMap
	overrideFname       string
	onChangeCallbacks   []func(cfg *Config)
}

type ConfigMap map[string]map[string]string

func (a *App) getConfigFilename() string {
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
	return configFname
}

func (cm *ConfigMap) loadFromIniFile(fname string) error {
	iniCfg, err := ini.LoadFile(fname)
	if err != nil {
		return err
	}
	for section, m := range iniCfg {
		for k, v := range m {
			cm.Add(section, k, v)
		}
	}

	return nil
}

func (cm *ConfigMap) loadFromJsonFile(fname string) error {
	overrideJsonBytes, err := ioutil.ReadFile(fname)
	if err != nil {
		return err
	}
	err = json.Unmarshal(overrideJsonBytes, cm)
	if err != nil {
		return err
	}
	return nil
}

func (cm *ConfigMap) saveToJsonFile(fname string) error {
	jsonBytes, err := json.Marshal(cm)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(fname, jsonBytes, 0644)
}

func (a *App) loadAppConfigFile() {
	// We do not have logging set up yet. We just panic() on error.
	configFname := a.getConfigFilename()

	source := make(ConfigMap)
	err := source.loadFromIniFile(configFname)
	if err != nil {
		// Can't log, it's all too early. This is fatal, tho
		panic(fmt.Sprintf("Can't load config file [%s]: %s", configFname, err.Error()))
	}

	persistentOverrides := make(ConfigMap)
	overrideFname := configFname + ".override"
	err = persistentOverrides.loadFromJsonFile(overrideFname)
	if err != nil {
		// Don't have logging yet, so use log. and hope
		log.Printf("Failed to load or parse override config file [%s]: %s\n", overrideFname, err.Error())
		// Don't want to fail here, just continue without overrides
		err = nil
	}

	a.Cfg = Config{
		source:              source,
		persistentOverrides: persistentOverrides,
		transientOverrides:  make(ConfigMap),
		overrideFname:       overrideFname,
		onChangeCallbacks:   make([]func(cfg *Config), 0),
	}
}

func (cfgMap *ConfigMap) Get(sName, k string, def string) (string, bool) {
	s, ok := map[string]map[string]string(*cfgMap)[sName]
	if !ok {
		return def, false
	}
	v, ok := map[string]string(s)[k]
	if !ok {
		return def, false
	}
	return v, true
}

func (cfgMap *ConfigMap) Add(sName, k, v string) {
	_, ok := (*cfgMap)[sName]
	if !ok {
		(*cfgMap)[sName] = make(map[string]string)
	}
	(*cfgMap)[sName][k] = v
}

func (cfgMap *ConfigMap) Sections() []string {
	sections := make([]string, 0)
	for k, _ := range *cfgMap {
		sections = append(sections, k)
	}
	return sections
}

func (cfgMap *ConfigMap) SectionKeys(sName string) []string {
	keys := make([]string, 0)
	section, ok := (*cfgMap)[sName]
	if !ok {
		return keys
	}
	for k, _ := range section {
		keys = append(keys, k)
	}
	return keys
}

func (cfg *Config) AddOnChangeCallback(f func(cfg *Config)) {
	cfg.onChangeCallbacks = append(cfg.onChangeCallbacks, f)
}

func (cfg *Config) notifyChange() {
	for _, f := range cfg.onChangeCallbacks {
		// These should be quick!
		f(cfg)
	}
}

func (cfg *Config) savePersistentOverrides() error {
	return cfg.persistentOverrides.saveToJsonFile(cfg.overrideFname)
}

func (cfg *Config) Sections() []string {
	sectionMap := make(map[string]bool)

	sourceSections := cfg.source.Sections()
	for _, section := range sourceSections {
		sectionMap[section] = true
	}
	for section := range cfg.persistentOverrides {
		sectionMap[section] = true
	}
	for section := range cfg.transientOverrides {
		sectionMap[section] = true
	}

	sections := make([]string, 0)
	for k, _ := range sectionMap {
		sections = append(sections, k)
	}
	return sections
}

func (cfg *Config) SectionKeys(sName string) []string {
	keyMap := make(map[string]bool)

	sourceKeys := cfg.source.SectionKeys(sName)
	for _, key := range sourceKeys {
		keyMap[key] = true
	}

	overrideSection, ok := cfg.persistentOverrides[sName]
	if ok {
		for key := range overrideSection {
			keyMap[key] = true
		}
	}

	overrideSection, ok = cfg.transientOverrides[sName]
	if ok {
		for key := range overrideSection {
			keyMap[key] = true
		}
	}

	keys := make([]string, 0)
	for k, _ := range keyMap {
		keys = append(keys, k)
	}
	return keys
}

func (cfg *Config) AsMap() map[string]map[string]string {
	configMap := make(map[string]map[string]string)
	sections := cfg.Sections()
	for _, section := range sections {
		configMap[section] = make(map[string]string)
		keys := cfg.SectionKeys(section)
		for _, key := range keys {
			configMap[section][key], _ = cfg.Get(section, key, "")
		}
	}
	return configMap
}

func (cfg *Config) PersistentOverride(sectionName, key, val string) {
	section, ok := cfg.persistentOverrides[sectionName]
	if !ok {
		cfg.persistentOverrides[sectionName] = make(map[string]string)
		section = cfg.persistentOverrides[sectionName]
	}
	section[key] = val
	err := cfg.savePersistentOverrides()
	if err != nil {
		log.Printf("Failed to save to override file [%s]: %s\n", cfg.overrideFname, err.Error())
	}
	cfg.notifyChange()
	return
}

func (cfg *Config) TransientOverride(sectionName, key, val string) {
	section, ok := cfg.transientOverrides[sectionName]
	if !ok {
		cfg.transientOverrides[sectionName] = make(map[string]string)
		section = cfg.transientOverrides[sectionName]
	}
	section[key] = val
	cfg.notifyChange()
	return
}

func (cfg *Config) Get(sectionName, key string, def string) (string, bool) {
	str, found := cfg.transientOverrides.Get(sectionName, key, def)
	if found {
		return str, true
	}
	str, found = cfg.persistentOverrides.Get(sectionName, key, def)
	if found {
		return str, true
	}

	// Not found, just punt it to the base
	return cfg.source.Get(sectionName, key, def)
}

func (cfg *Config) GetInt(sName, k string, def int) (int, bool) {
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
func (cfg *Config) GetInt64(sName, k string, def int64) (int64, bool) {
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
func (cfg *Config) GetBool(sName, k string, def bool) (bool, bool) {
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
func (cfg *Config) GetFloat32(sName, k string, def float32) (float32, bool) {
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
func (cfg *Config) GetList(sName, k string, def []string) ([]string, bool) {
	vStr, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	v := strings.Split(vStr, ",")
	for i := 0; i < len(v); i++ {
		v[i] = strings.TrimSpace(v[i])
	}
	return v, true
}
func (cfg *Config) GetDuration(sName, k string, def time.Duration) (time.Duration, bool) {
	vStr, found := cfg.Get(sName, k, "")
	if !found {
		return def, false
	}
	v, err := time.ParseDuration(vStr)
	if err != nil {
		return def, false
	}
	return v, true
}

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
	Get(sectionName, optionName string, defaultValue string) (string, bool)
	Add(sectionName, optionName, optionValue string)
	Sections() []string
	SectionKeys(sectionName string) []string
}

type Config struct {
	source              ConfigMap
	persistentOverrides ConfigMap
	transientOverrides  ConfigMap
	overrideFname       string
	onChangeCallbacks   []func(cfg *Config)
}

type ConfigMap map[string]map[string]string

func (a *App) getConfigFilename(forceCurrentWorkingDir bool) string {

	rootEnvName := strings.ToUpper(a.ProjectName) + "_CFG_ROOT"
	configRoot := os.Getenv(rootEnvName)
	if configRoot == "" {
		configRoot = "/etc/" + a.ProjectName
	}

	if forceCurrentWorkingDir {
		configRoot = "."
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
	source := make(ConfigMap)

	configFname := a.getConfigFilename(false)
	err := source.loadFromIniFile(configFname)
	if err != nil && !os.IsNotExist(err) {
		// Can't log, it's all too early. This is fatal, tho
		panic(fmt.Sprintf("Can't load config file [%s]: %s", configFname, err.Error()))
	}

	if err != nil {
		// Try again in cwd
		configFname = a.getConfigFilename(true)
		err = source.loadFromIniFile(configFname)
		if err != nil {
			// Can't log, it's all too early. This is fatal, tho
			panic(fmt.Sprintf("Can't load config file [%s] after fallback to cwd: %s", configFname, err.Error()))
		}
	}

	persistentOverrides := make(ConfigMap)
	overrideFname := configFname + ".override"
	fi, err := os.Stat(overrideFname)
	if err == nil && fi.Size() > 0 {
		err = persistentOverrides.loadFromJsonFile(overrideFname)
		if err != nil {
			// Don't have logging yet, so use log. and hope
			log.Printf("Failed to load or parse override config file [%s]: %s\n", overrideFname, err.Error())
			// Don't want to fail here, just continue without overrides
			err = nil
		}
	}

	a.Cfg = Config{
		source:              source,
		persistentOverrides: persistentOverrides,
		transientOverrides:  make(ConfigMap),
		overrideFname:       overrideFname,
		onChangeCallbacks:   make([]func(cfg *Config), 0),
	}
}

// Get an option value for the given sectionName.
// Will return defaultValue if the section or the option does not exist.
// The second return value is True if the requested option value was returned and False if the default value was returned.
func (cfgMap *ConfigMap) Get(sectionName, optionName string, defaultValue string) (string, bool) {
	s, ok := map[string]map[string]string(*cfgMap)[sectionName]
	if !ok {
		return defaultValue, false
	}
	v, ok := map[string]string(s)[optionName]
	if !ok {
		return defaultValue, false
	}
	return v, true
}

// Set the given option to the specified value for the named section.
// Create the section if it does not exist.
func (cfgMap *ConfigMap) Add(sectionName, optionName, optionValue string) {
	_, ok := (*cfgMap)[sectionName]
	if !ok {
		(*cfgMap)[sectionName] = make(map[string]string)
	}
	(*cfgMap)[sectionName][optionName] = optionValue
}

// Get a list of the names of the avaliable sections.
func (cfgMap *ConfigMap) Sections() []string {
	sections := make([]string, 0)
	for k, _ := range *cfgMap {
		sections = append(sections, k)
	}
	return sections
}

// Get a list of options for the named section.
// Will return an empty list if the section does not exist.
func (cfgMap *ConfigMap) SectionKeys(sectionName string) []string {
	keys := make([]string, 0)
	section, ok := (*cfgMap)[sectionName]
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

// Get a list of the names of the available sections, including those specified in the override file.
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

// Get a list of options for the named section, including those specified in the override file.
func (cfg *Config) SectionKeys(sectionName string) []string {
	keyMap := make(map[string]bool)

	sourceKeys := cfg.source.SectionKeys(sectionName)
	for _, key := range sourceKeys {
		keyMap[key] = true
	}

	overrideSection, ok := cfg.persistentOverrides[sectionName]
	if ok {
		for key := range overrideSection {
			keyMap[key] = true
		}
	}

	overrideSection, ok = cfg.transientOverrides[sectionName]
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

// Get a copy of the config as a map that maps each section to a map that maps the options to the values.
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

func (cfg *Config) PersistentOverride(sectionName, optionName, optionValue string) {
	section, ok := cfg.persistentOverrides[sectionName]
	if !ok {
		cfg.persistentOverrides[sectionName] = make(map[string]string)
		section = cfg.persistentOverrides[sectionName]
	}
	section[optionName] = optionValue
	err := cfg.savePersistentOverrides()
	if err != nil {
		log.Printf("Failed to save to override file [%s]: %s\n", cfg.overrideFname, err.Error())
	}
	cfg.notifyChange()
	return
}

func (cfg *Config) TransientOverride(sectionName, optionName, optionValue string) {
	section, ok := cfg.transientOverrides[sectionName]
	if !ok {
		cfg.transientOverrides[sectionName] = make(map[string]string)
		section = cfg.transientOverrides[sectionName]
	}
	section[optionName] = optionValue
	cfg.notifyChange()
	return
}

func (cfg *Config) Get(sectionName, optionName string, defaultValue string) (string, bool) {
	str, found := cfg.transientOverrides.Get(sectionName, optionName, defaultValue)
	if found {
		return str, true
	}
	str, found = cfg.persistentOverrides.Get(sectionName, optionName, defaultValue)
	if found {
		return str, true
	}

	// Not found, just punt it to the base
	return cfg.source.Get(sectionName, optionName, defaultValue)
}

// Same as Config.Get, but returns the value as int.
func (cfg *Config) GetInt(sectionName, optionName string, defaultValue int) (int, bool) {
	v, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	r, err := strconv.Atoi(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", optionName, v, err))
}

// Same as Config.Get, but returns the value as int64.
// The integer has to be written in the config in decimal format. This means that for the value written in
// the config as "08" this method will return 8 instead of 10. And "0x8" will generate an error.
func (cfg *Config) GetInt64(sectionName, optionName string, defaultValue int64) (int64, bool) {
	v, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	r, err := strconv.ParseInt(v, 10, 64)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Non-numeric config key %s: %s [%s]", optionName, v, err))
}

// Same as Config.Get, but returns the value as boolean.
// The option value should be one that strconv.ParseBool understands.
func (cfg *Config) GetBool(sectionName, optionName string, defaultValue bool) (bool, bool) {
	v, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	r, err := strconv.ParseBool(v)
	if err == nil {
		return r, true
	}
	panic(fmt.Sprintf("Bad boolean config key %s: %s", optionName, v))
}

// Same as Config.Get, but returns the value as float32.
func (cfg *Config) GetFloat32(sectionName, optionName string, defaultValue float32) (float32, bool) {
	v, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	r, err := strconv.ParseFloat(v, 32)
	if err == nil {
		return float32(r), true
	}
	panic(fmt.Sprintf("Non-numeric float32 config key %s: %s [%s]", optionName, v, err))
}

// Return a list of strings for a config value that is written as a comma-separated list.
// Each value will be stripped out of leading and trailing white spaces as defined by Unicode.
func (cfg *Config) GetList(sectionName, optionName string, defaultValue []string) ([]string, bool) {
	vStr, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	v := strings.Split(vStr, ",")
	for i := 0; i < len(v); i++ {
		v[i] = strings.TrimSpace(v[i])
	}
	return v, true
}

// Same as Config.Get but returns the value as time.Duration.
// The value in the config file should be in the format that time.ParseDuration() understands.
func (cfg *Config) GetDuration(sectionName, optionName string, defaultValue time.Duration) (time.Duration, bool) {
	vStr, found := cfg.Get(sectionName, optionName, "")
	if !found {
		return defaultValue, false
	}
	v, err := time.ParseDuration(vStr)
	if err != nil {
		return defaultValue, false
	}
	return v, true
}

func (cfg *Config) GetMap(sectionName, kPrefix string, defaultValue map[string]string) (map[string]string, bool) {
	keys := cfg.SectionKeys(sectionName)
	v := make(map[string]string)
	for _, k := range keys {
		if strings.HasPrefix(k, kPrefix) {
			kTrimmed := strings.TrimPrefix(k, kPrefix)
			v[kTrimmed], _ = cfg.Get(sectionName, k, "")
		}
	}
	found := len(v) > 0
	if !found {
		return defaultValue, false
	}
	return v, true
}

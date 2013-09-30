package gop

import (
    "github.com/vaughan0/go-ini"

    "fmt"
    "os"
    "strings"
    "strconv"
)

type Config map[string]Section

type Section map[string]string

func (a *App) loadAppConfigFile() {

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

    theCfg := make(Config)
    for section, m := range cfg {
        theCfg[section] = make(map[string]string)
        for k, v := range m {
            theCfg[section][k] = v
        }
    }
    a.Cfg = &theCfg
}

func (cfg *Config) Get(sName, k string, def string) (string, bool) {
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

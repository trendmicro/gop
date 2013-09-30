package gop

import (
    "github.com/cactus/go-statsd-client/statsd"
)

type StatsdClient struct {
    client *statsd.Client
    rate float32
}

func (a *App) initStatsd() {
    statsdHostport, _ := a.Cfg.Get("gop", "statsd_hostport", "localhost:8125")
    statsdPrefix := a.AppName + "." + a.ProjectName
    client, err := statsd.New(statsdHostport, statsdPrefix)
    if err != nil {
        // App will probably fall over due to nil client. That's OK.
        // We *could* panic below, but lets try and continue at least
        a.Error("Failed to create statsd client: " + err.Error())
        return
    }

    rate, _ := a.Cfg.GetFloat32("gop", "statsd_rate", 1.0)
    a.Stats = StatsdClient{
        client: client,
        rate: rate,
    }
}

func (s *StatsdClient) Dec(stat string, value int64) {
    _ = s.client.Dec(stat, value, s.rate)
}

func (s *StatsdClient) Gauge(stat string, value int64) {
    _ = s.client.Gauge(stat, value, s.rate)
}

func (s *StatsdClient) GaugeDelta(stat string, value int64) {
    _ = s.client.GaugeDelta(stat, value, s.rate)
}

func (s *StatsdClient) Inc(stat string, value int64) {
    _ = s.client.Inc(stat, value, s.rate)
}

func (s *StatsdClient) Timing(stat string, delta int64) {
    _ = s.client.Timing(stat, delta, s.rate)
}

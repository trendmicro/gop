package gop

import (
	"strings"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
)

type StatsdClient interface {
	Dec(stat string, value int64)
	Gauge(stat string, value int64)
	GaugeDelta(stat string, value int64)
	Inc(stat string, value int64)
	Timing(stat string, delta int64)
	TimingDuration(stat string, delta time.Duration)
	// TimingTrack records the time something took to run using a defer,
	// very good for timing a function call, just add a the defer at the
	// top like so:
	//     func timeMe() {
	//         defer app.Stats.TimingTrack("timeMe.run_time", time.Now())
	//         // rest of code, can return anywhere and run time tracked
	//     }
	// This works because the time.Now() is run when defer line is reached,
	// so gets the time timeMe started.
	TimingTrack(stat string, start time.Time)
}

func (a *App) initStatsd() {
	a.configureStatsd(nil)
	a.Cfg.AddOnChangeCallback(a.configureStatsd)
}

func (a *App) configureStatsd(cfg *Config) {
	statsdHostport, _ := a.Cfg.Get("gop", "statsd_hostport", "localhost:8125")
	prefix := []string{}
	if p, ok := a.Cfg.Get("gop", "statsd_prefix", ""); ok {
		prefix = append(prefix, p)
	}
	hostname := strings.Replace(a.Hostname(), ".", "_", -1)
	prefix = append(prefix, a.ProjectName, a.AppName, hostname)
	statsdPrefix := strings.Join(prefix, ".")
	a.Fine("STATSD PREFIX %s", statsdPrefix)
	client, err := statsd.New(statsdHostport, statsdPrefix)
	if err != nil {
		a.Error("Failed to create network statsd client: " + err.Error())
		// Create a null client that does nothing.
		a.Stats = NullStatsdClient{}
		return
	}

	rate, _ := a.Cfg.GetFloat32("gop", "statsd_rate", 1.0)
	// TODO: Need to protect a.Stats from race
	a.Stats = NetworkStatsdClient{
		client: client,
		rate:   rate,
		app:    a,
	}

	a.Finef("STATSD sending to [%s] with prefix [%s] at rate [%f]", statsdHostport, statsdPrefix, rate)
}

type NetworkStatsdClient struct {
	client statsd.Statter
	rate   float32
	app    *App
}

func (s NetworkStatsdClient) Dec(stat string, value int64) {
	s.app.Fine("STATSD DEC %s %d", stat, value)
	_ = s.client.Dec(stat, value, s.rate)
}

func (s NetworkStatsdClient) Gauge(stat string, value int64) {
	s.app.Fine("STATSD GAUGE %s %d", stat, value)
	_ = s.client.Gauge(stat, value, s.rate)
}

func (s NetworkStatsdClient) GaugeDelta(stat string, value int64) {
	s.app.Fine("STATSD GAUGEDELTA %s %d", stat, value)
	_ = s.client.GaugeDelta(stat, value, s.rate)
}

func (s NetworkStatsdClient) Inc(stat string, value int64) {
	s.app.Fine("STATSD INC %s %d", stat, value)
	_ = s.client.Inc(stat, value, s.rate)
}

func (s NetworkStatsdClient) Timing(stat string, delta int64) {
	s.app.Fine("STATSD TIMING %s %d", stat, delta)
	_ = s.client.Timing(stat, delta, s.rate)
}

func (s NetworkStatsdClient) TimingDuration(stat string, delta time.Duration) {
	s.app.Fine("STATSD TIMING %s %s", stat, delta)
	_ = s.client.TimingDuration(stat, delta, s.rate)
}

func (s NetworkStatsdClient) TimingTrack(stat string, start time.Time) {
	elapsed := time.Since(start)
	s.app.Finef("STATSD TIMING %s %s", stat, elapsed)
	_ = s.client.TimingDuration(stat, elapsed, s.rate)
}

type NullStatsdClient struct{}

func (s NullStatsdClient) Dec(stat string, value int64) {
}

func (s NullStatsdClient) Gauge(stat string, value int64) {
}

func (s NullStatsdClient) GaugeDelta(stat string, value int64) {
}

func (s NullStatsdClient) Inc(stat string, value int64) {
}

func (s NullStatsdClient) Timing(stat string, delta int64) {
}

func (s NullStatsdClient) TimingDuration(stat string, delta time.Duration) {
}

func (s NullStatsdClient) TimingTrack(stat string, start time.Time) {
}

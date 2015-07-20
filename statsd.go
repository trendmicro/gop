package gop

import (
	"os"
	"strings"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
)

type StatsdClient struct {
	client statsd.Statter
	rate   float32
	app    *App
}

func (a *App) initStatsd() {
	statsdHostport, _ := a.Cfg.Get("gop", "statsd_hostport", "localhost:8125")
	prefix := []string{}
	if p, ok := a.Cfg.Get("gop", "statsd_prefix", ""); ok {
		prefix = append(prefix, p)
	}
	hostname, _ := os.Hostname()
	prefix = append(prefix, a.ProjectName, a.AppName, strings.Replace(hostname, ".", "_", -1))
	statsdPrefix := strings.Join(prefix, ".")
	a.Debug("STATSD PREFIX %s", statsdPrefix)
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
		rate:   rate,
		app:    a,
	}
}

func (s *StatsdClient) Dec(stat string, value int64) {
	s.app.Debug("STATSD DEC %s %d", stat, value)
	_ = s.client.Dec(stat, value, s.rate)
}

func (s *StatsdClient) Gauge(stat string, value int64) {
	s.app.Debug("STATSD GAUGE %s %d", stat, value)
	_ = s.client.Gauge(stat, value, s.rate)
}

func (s *StatsdClient) GaugeDelta(stat string, value int64) {
	s.app.Debug("STATSD GAUGEDELTA %s %d", stat, value)
	_ = s.client.GaugeDelta(stat, value, s.rate)
}

func (s *StatsdClient) Inc(stat string, value int64) {
	s.app.Debug("STATSD INC %s %d", stat, value)
	_ = s.client.Inc(stat, value, s.rate)
}

func (s *StatsdClient) Timing(stat string, delta int64) {
	s.app.Debug("STATSD TIMING %s %d", stat, delta)
	_ = s.client.Timing(stat, delta, s.rate)
}

func (s *StatsdClient) TimingDuration(stat string, delta time.Duration) {
	s.app.Debug("STATSD TIMING %s %s", stat, delta)
	_ = s.client.TimingDuration(stat, delta, s.rate)
}

// TimingTrack records the time something took to run using a defer, very good
// for timing a function call, just add a defer at the top like so:
//     func timeMe() {
//         defer app.Stats.TimingTrack("timeMe.run_time", time.Now())
//         // rest of code, can return anywhere and run time tracked
//     }
func (s *StatsdClient) TimingTrack(stat string, start time.Time) {
	elapsed := time.Since(start)
	s.app.Debugf("STATSD TIMING %s %s", stat, elapsed)
	_ = s.client.TimingDuration(stat, elapsed, s.rate)
}

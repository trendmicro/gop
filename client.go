package gop

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/cocoonlife/timber"
)

// Status decodes the JSON status url return
type StatusInfo struct {
	AppName       string
	ProjectName   string
	Pid           int
	StartTime     time.Time
	UptimeSeconds float64
	NumGoros      int
	RequestInfo   []RequestInfo
}

func (status StatusInfo) String() string {
	return fmt.Sprintf("%s:%s started %s, up %0.2fs, pid %d, %d reqs, %d goros",
		status.ProjectName,
		status.AppName,
		status.StartTime,
		status.UptimeSeconds,
		status.Pid,
		len(status.RequestInfo),
		status.NumGoros)
}

// RequestInfo details of an open request
type RequestInfo struct {
	Id       int
	Method   string
	Url      string
	Duration float64
	RemoteIP string
	IsHTTPS  bool
}

func (r RequestInfo) String() string {
	s := fmt.Sprintf("[%d] ", r.Id)
	if r.IsHTTPS {
		s += "https "
	} else {
		s += "http "
	}
	s += fmt.Sprintf("%s %s %fs (%s)", r.Method, r.Url, r.Duration, r.RemoteIP)
	return s
}

type MemInfo runtime.MemStats

func (mem MemInfo) String() (s string) {
	s += fmt.Sprintf("Alloc = %dbytes\n", mem.Alloc)
	s += fmt.Sprintf("TotalAlloc = %dbytes\n", mem.TotalAlloc)
	s += fmt.Sprintf("Sys = %d\nbytes", mem.Sys)
	s += fmt.Sprintf("Lookups = %d\n", mem.Lookups)
	s += fmt.Sprintf("Mallocs = %d\n", mem.Mallocs)
	// Main allocation heap
	s += fmt.Sprintf("Frees = %dbytes\n", mem.Frees)
	s += fmt.Sprintf("HeapAlloc = %dbytes\n", mem.HeapAlloc)
	s += fmt.Sprintf("HeapSys = %dbytes\n", mem.HeapSys)
	s += fmt.Sprintf("HeapIdle = %dbytes\n", mem.HeapIdle)
	s += fmt.Sprintf("HeapInuse = %dbytes\n", mem.HeapInuse)
	s += fmt.Sprintf("HeapReleased = %dbytes\n", mem.HeapReleased)
	s += fmt.Sprintf("HeapObjects = %d\n", mem.HeapObjects)

	s += fmt.Sprintf("StackInuse = %dbytes\n", mem.StackInuse)
	s += fmt.Sprintf("StackSys = %dbytes\n", mem.StackSys)
	s += fmt.Sprintf("MSpanInuse = %dbytes\n", mem.MSpanInuse)
	s += fmt.Sprintf("MSpanSys = %dbytes\n", mem.MSpanSys)
	s += fmt.Sprintf("MCacheInuse = %dbytes\n", mem.MCacheInuse)
	s += fmt.Sprintf("MCacheSys = %dbytes\n", mem.MCacheSys)
	s += fmt.Sprintf("BuckHashSys = %dbytes\n", mem.BuckHashSys)
	s += fmt.Sprintf("GCSys = %dbytes\n", mem.GCSys)
	s += fmt.Sprintf("OtherSys = %dbytes\n", mem.OtherSys)

	s += fmt.Sprintf("NextGC = %d\n", mem.NextGC)
	s += fmt.Sprintf("LastGC = %d\n", mem.LastGC)
	s += fmt.Sprintf("PauseTotalNs = %d\n", mem.PauseTotalNs)
	s += fmt.Sprintf("NumGC = %d\n", mem.NumGC)
	s += fmt.Sprintf("EnableGC = %t\n", mem.EnableGC)
	s += fmt.Sprintf("DebugGC = %t\n", mem.DebugGC)
	return s
}

// StringInfo represents a go runtime stack.
type StackInfo struct {
	lines []string
	goro  []*GoroInfo
}

// ParseStackInfo produces a StackInfo struct by parsing a stack string.
func ParseStackInfo(txt string) StackInfo {
	stack := StackInfo{}
	stack.lines = strings.Split(txt, "\n")
	var cur *GoroInfo
	for _, l := range stack.lines {
		if strings.HasPrefix(l, "goroutine") {
			cur = &GoroInfo{}
			stack.goro = append(stack.goro, cur)
		}
		if cur != nil {
			cur.lines = append(cur.lines, l)
		}
	}
	return stack
}

func (si StackInfo) String() string {
	return strings.Join(si.lines, "\n")
}

// Goros returns list of goroutines in the stack.
func (si StackInfo) Goros() (gr []*GoroInfo) {
	return si.goro
}

// GoroInfo contains the info on a single goro from a stack parsed by StackInfo.
type GoroInfo struct {
	lines []string
}

func (gi GoroInfo) String() string {
	return strings.Join(gi.lines, "\n")
}

// Head returns the 1st line of the goro info.
func (gi GoroInfo) Head() string {
	return gi.lines[0]
}

// RoutineLines returns the all the lines after the first.
func (gi GoroInfo) RoutineLines() []string {
	return gi.lines[1:]
}

// Client to access the web api exposed under the gop/ url. For use by code
// that wants to talk to your gop applications. See the gopctl commandline
// tool, which wraps this client.
type Client struct {
	AppURL url.URL
}

// NewClient creates a new client for given gop app. Pass a string url pointing
// at the application, e.g. http://localhost:2342/gop. You can drop the /gop
// bit of the path and use a root url, /gop get added automatically.
// You can drop http:// and it will be added for you.
// If string starts with a : it is assumed to be a url starting with the port,
// on localhost.
//
//
//	// These all point to the same app
//	c, err := NewClient("http://localhost:2342/gop")
//	c, err := NewClient("http://localhost:2342")
//	c, err := NewClient("localhost:2342")
//	c, err := NewClient(":2342")
//
func NewClient(appstr string) (client *Client, err error) {
	if !strings.HasPrefix(appstr, "http") {
		if strings.HasPrefix(appstr, ":") { // port only given
			appstr = "http://localhost" + appstr
		} else {
			appstr = "http://" + appstr
		}
	}
	appstr = strings.TrimSuffix(appstr, "/")
	if !strings.HasSuffix(appstr, "gop") {
		appstr = appstr + "/gop"
	}
	u, err := url.Parse(appstr)
	if err != nil {
		return nil, err
	}
	return &Client{
		AppURL: *u, // note copy
	}, nil
}

// GetText gets the path from the path given relative to the app URL, returning
// it as a string.
func (c *Client) GetText(path string) (txt string, err error) {
	url := c.AppURL
	url.Path += ("/" + path)
	if resp, err := http.Get(url.String()); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("GET %s failed: Status:%s", url, resp.Status)
		}
		var body []byte
		if body, err = ioutil.ReadAll(resp.Body); err == nil {
			return string(body), nil
		} else {
			return "", err
		}
	} else {
		return "", err
	}
}

// GetJSON gets the path from the path given relative to the app URL, parsing
// it into the interface v given.
func (c *Client) GetJSON(path string, v interface{}) (err error) {
	url := c.AppURL
	url.Path += ("/" + path)
	if resp, err := http.Get(url.String()); err == nil {
		defer resp.Body.Close()
		timber.Debugf("GET %s\n %s", url, resp.Status)
		if body, err := ioutil.ReadAll(resp.Body); err == nil {
			err = json.Unmarshal(body, v)
			return err
		} else if resp.StatusCode != 200 {
			return fmt.Errorf("%s failed. Status:%s", url.String(), resp.Status)
		}
	} else {
		timber.Debugf("GET %s Error:%s\n", url, err.Error())
		return err
	}
	return err
}

// Status reads the gop/status url.
func (c *Client) Status() (status StatusInfo, err error) {
	err = c.GetJSON("status", &status)
	return status, err
}

// Stack reads the gop/stack url.
func (c *Client) Stack() (stack StackInfo, err error) {
	if txt, err := c.GetText("stack"); err == nil {
		stack = ParseStackInfo(txt)
	}
	return stack, err
}

// Mem reads the gop/mem url.
func (c *Client) Mem() (mem MemInfo, err error) {
	err = c.GetJSON("mem", &mem)
	return mem, err
}

// Cfg reads the config gop/url returning the current ConfigMap.
func (c *Client) Cfg() (cfg ConfigMap, err error) {
	err = c.GetJSON("config", &cfg)
	return cfg, err
}

// SetCfg updates the given section key with a new value by doing a PUT to the
// gop/config/{section}/{key} url.
func (c *Client) SetCfg(section, key, val string) (resptxt string, err error) {
	url := c.AppURL
	url.Path += fmt.Sprintf("/config/%s/%s", section, key)
	client := &http.Client{}
	request, err := http.NewRequest("PUT", url.String(), strings.NewReader(val))
	request.ContentLength = int64(len(val))
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode != 200 {
		return "", fmt.Errorf("ERROR. Status: %d", response.StatusCode)
	}
	return string(contents), nil
}

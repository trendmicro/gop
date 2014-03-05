package gop

import (
	"fmt"
	"github.com/jbert/timber"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Logger timber.Logger

func (a *App) initLogging() {

	defaultLogPattern := "[%D %T] [%L] %M"
	filenamesByDefault, _ := a.Cfg.GetBool("gop", "log_filename", false)
	if filenamesByDefault {
		defaultLogPattern = "[%D %T] [%L] %S %M"
	}
	logPattern, _ := a.Cfg.Get("gop", "log_pattern", defaultLogPattern)

	// If set, hack all logging to stdout for dev
	forceStdout, _ := a.Cfg.GetBool("gop", "stdout_only_logging", false)
	configLogger := timber.ConfigLogger{
		LogWriter: new(timber.ConsoleWriter),
		Level:     timber.INFO,
		Formatter: timber.NewPatFormatter(logPattern),
	}

	defaultLogDir, _ := a.Cfg.Get("gop", "log_dir", "/var/log")
	fellbackToCWD := false
	logDir := defaultLogDir + "/" + a.ProjectName
	if !forceStdout {
		defaultLogFname := logDir + "/" + a.AppName + ".log"
		logFname, _ := a.Cfg.Get("gop", "log_file", defaultLogFname)

		newWriter, err := timber.NewFileWriter(logFname)
		_, dirExistsErr := os.Stat(logDir)
		if dirExistsErr != nil && os.IsNotExist(dirExistsErr) {
			// Carry on with stdout logging, but remember to mention it
			fellbackToCWD = true
		} else {
			if err != nil {
				panic(fmt.Sprintf("Can't open log file: %s", err))
			}
			configLogger.LogWriter = newWriter
		}
	}

	logLevelStr, _ := a.Cfg.Get("gop", "log_level", "INFO")
	logLevelStr = strings.ToUpper(logLevelStr)
	for logLevel, levelStr := range timber.LongLevelStrings {
		if logLevelStr == levelStr {
			configLogger.Level = timber.Level(logLevel)
			break
		}
	}

	// *Don't* create a NewTImber here. Logs are only flushed on Close() and if we
	// have more than one timber, it's easy to only Close() one of them...
	l := timber.Global
	l.AddLogger(configLogger)
	a.Logger = l

	// Set up the default go logger to go here too, so 3rd party
	// module logging plays nicely
	log.SetFlags(0)
	log.SetOutput(l)

	doAccessLog, _ := a.Cfg.GetBool("gop", "access_log_enable", false)
	if doAccessLog {
		defaultAccessLogFname := defaultLogDir + "/" + a.ProjectName + "/" + a.AppName + "-access.log"
		accessLogFilename, _ := a.Cfg.Get("gop", "access_log_filename", defaultAccessLogFname)
		// Don't use .Create since it truncates
		var err error
		a.accessLog, err = os.OpenFile(accessLogFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			l.Error("Can't open access log; %s", err.Error())
		}
	}

	if fellbackToCWD {
		l.Error("Logging directory [%s] does not exist - logging to stdout", logDir)
	}
}

func (a *App) closeLogging() {
	if a.accessLog != nil {
		err := a.accessLog.Close()
		if err != nil {
			a.Error("Error closing access log: %s", err.Error())
		}
	}
	timber.Close()
}

func (a *App) WriteAccessLog(req *Req, dur time.Duration) {
	if a.accessLog == nil {
		return
	}
	logEvery, _ := a.Cfg.GetInt("gop", "access_log_every", 0)
	if logEvery > 0 {
		a.suppressedAccessLogLines++
		if a.suppressedAccessLogLines < logEvery {
			a.Debug("Suppressing access log line [%d/%d]", a.suppressedAccessLogLines, logEvery)
			return
		}
	}
	a.suppressedAccessLogLines = 0

	// Copy an nginx-log access log
	/* ---
	   gaiadev.leedsdev.net 0.022 192.168.111.1 - - [05/Feb/2014:13:39:22 +0000] "GET /bby/sso/login?next_url=https%3A%2F%2Fgaiadev.leedsdev.net%2F HTTP/1.1" 302 0 "-" "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:26.0) Gecko/20100101 Firefox/26.0"
	   --- */
	trimPort := func(s string) string {
		colonOffset := strings.IndexByte(s, ':')
		if colonOffset >= 0 {
			s = s[:colonOffset]
		}
		return s
	}
	quote := func(s string) string {
		return string(strconv.AppendQuote([]byte{}, s))
	}

	reqFirstLine := fmt.Sprintf("%s %s %s", req.r.Method, req.r.RequestURI, req.r.Proto)
	referrerLine := req.r.Referer()
	if referrerLine == "" {
		referrerLine = "-"
	}
	uaLine := req.r.Header.Get("User-Agent")
	if uaLine == "" {
		uaLine = "-"
	}
	hostname, _ := os.Hostname()
	logLine := fmt.Sprintf("%s %.3f %s %s %s %s %s %d %d %s %s\n",
		hostname,
		dur.Seconds(),
		trimPort(req.RealRemoteIP),
		"-", // Ident <giggle>
		"-", // user
		//		req.startTime.Format("[02/Jan/2006:15:04:05 -0700]"),
		req.startTime.Format("["+time.RFC3339+"]"),
		quote(reqFirstLine),
		req.writer.code,
		req.writer.size,
		quote(referrerLine),
		quote(uaLine))
	_, err := req.app.accessLog.WriteString(logLine)
	if err != nil {
		a.Error("Failed to write to access log: %s", err.Error())
	}
}

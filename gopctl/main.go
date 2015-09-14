package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/cocoonlife/gop"
	"github.com/spf13/cobra"
)

// cmd line for gop apps, set config, view memory etc
type GopCtl struct {
	*cobra.Command
	C       *gop.Client
	gopApp  string
	Host    string
	Port    int
	Created bool
}

type cmdFunc func(cmd *cobra.Command, args []string)

// NewGopCtl creates an new GopCtl reader to Execute.
func NewGopCtl() *GopCtl {
	ctl := &GopCtl{}
	ctl.Command = &cobra.Command{
		Use:   "gopctl",
		Short: "Command line tool to remote control a gop app.",
		Example: `	gopctl --app=http://localhost:1732 status
	gopctl --host=localhost --port=1732 mem
	gopctl -P1742 stack

	gopctl -P1732 get
	gopctl -P1732 get gop
	gopctl -P1732 get gop log_level

	gopctl -P1732 set gop log_level debug

	gopctl -H192.168.1.123 -P1742 top`,
		Run:              func(cmd *cobra.Command, args []string) { cmd.Help() },
		PersistentPreRun: ctl.preRun,
	}
	// Persistent flags availiable to all (sub) commands.
	ctl.PersistentFlags().StringVarP(&ctl.gopApp, "app", "", "http://localhost:1732", "base URL for the gop process you want to control")
	ctl.PersistentFlags().IntVarP(&ctl.Port, "port", "P", 1732, "port of gop app")
	ctl.PersistentFlags().StringVarP(&ctl.Host, "host", "H", "localhost", "host of gop app")

	ctl.addCmd("status", "Print the current app status.", func(cmd *cobra.Command, args []string) {
		fmt.Print(ctl.Status())
	})

	ctl.addCmd("mem", "Print the current memory usage", func(cmd *cobra.Command, args []string) {
		fmt.Print(ctl.Mem())
	})

	ctl.addCmd("stack", "Print the current go stack", func(cmd *cobra.Command, args []string) {
		fmt.Print(ctl.Stack())
	})

	c := ctl.addCmd("get [section] [key]", "Get all config, a sections config or key value", ctl.cfgGet)
	c.Aliases = append(c.Aliases, "cfg")

	ctl.addCmd("set <section> <key> <value>", "Set a config keys value", ctl.cfgSet)

	ctl.addCmd("requests", "Print the list of current active requests", ctl.requests)

	c = ctl.addCmd("goro", "Print the list of current goroutines", ctl.goros)
	c.Flags().BoolVar(&ctl.Created, "created", false, "show creating routine for each goro.")

	ctl.addCmd("top", "Top like summary", ctl.top)

	c = &cobra.Command{
		Use:     "raw PATH",
		Short:   "Get raw responses.",
		Example: "gopctl raw status\ngopctl --app :1742 raw mem | jq .Alloc",
		Run: func(cmd *cobra.Command, args []string) {
			path := args[0]
			txt, err := ctl.C.GetText(path)
			if err != nil {
				log.Fatalf(err.Error())
			}
			fmt.Print(txt)
		},
	}
	ctl.AddCommand(c)

	return ctl
}

// preRun for all commands, sets up the client from options
func (ctl *GopCtl) preRun(cmd *cobra.Command, args []string) {
	u, _ := url.Parse(ctl.gopApp)
	u.Host = fmt.Sprintf("%s:%d", ctl.Host, ctl.Port)
	if c, err := gop.NewClient(u.String()); err != nil {
		log.Fatalf("Failed to create client: %s", err.Error())
	} else {
		ctl.C = c
	}
}

func (ctl *GopCtl) addCmd(use, short string, run func(cmd *cobra.Command, args []string)) *cobra.Command {
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Run:   run,
	}
	ctl.AddCommand(c)
	return c
}

// Status wraps client Status call, logging any fail at fatal
func (ctl *GopCtl) Status() gop.StatusInfo {
	status, err := ctl.C.Status()
	if err != nil {
		log.Fatalf("Failed to get status: %s", err.Error())
	}
	return status
}

// Stack wraps client Stack call, logging any fail at fatal
func (ctl *GopCtl) Stack() gop.StackInfo {
	stack, err := ctl.C.Stack()
	if err != nil {
		log.Fatal("Failed to get stack: ", err.Error())
	}
	return stack
}

// Mem wraps client Mem call, logging any fail at fatal
func (ctl *GopCtl) Mem() gop.MemInfo {
	mem, err := ctl.C.Mem()
	if err != nil {
		log.Fatal("Failed to get mem: ", err.Error())
	}
	return mem
}

// Cfg wraps the client Cfg call, logging any fail at fatal
func (ctl *GopCtl) Cfg() gop.ConfigMap {
	cfg, err := ctl.C.Cfg()
	if err != nil {
		log.Fatalf("Failed to get config : %s", err.Error())
	}
	return cfg
}

func (c *GopCtl) cfgGet(cmd *cobra.Command, args []string) {
	// Grab the config
	cfg := c.Cfg()

	switch len(args) {
	case 0:
		for _, section := range cfg.Sections() {
			fmt.Printf("[%s]\n", section)
			for _, key := range cfg.SectionKeys(section) {
				if v, ok := cfg.Get(section, key, "?"); ok {
					fmt.Printf("%s = %s\n", key, v)
				}
			}
			fmt.Printf("\n")
		}
	case 1:
		section := args[0]
		for _, key := range cfg.SectionKeys(section) {
			if v, ok := cfg.Get(section, key, "?"); ok {
				fmt.Printf("%s = %s\n", key, v)
			}
		}
	case 2:
		section := args[0]
		key := args[1]
		if v, ok := cfg.Get(section, key, "?"); ok {
			fmt.Printf("%s\n", v)
		} else {
			fmt.Printf("NOT SET")
		}
	default:
		log.Fatal("Too many args!")
	}
}

func (c *GopCtl) cfgSet(cmd *cobra.Command, args []string) {
	if len(args) != 3 {
		log.Fatal("Need section, key and value args")
	}
	section := args[0]
	key := args[1]
	val := args[2]
	contents, err := c.C.SetCfg(section, key, val)
	if err != nil {
		log.Fatal("Failed to set: ", err.Error())
	}
	fmt.Print(string(contents))
}

func (ctl *GopCtl) requests(cmd *cobra.Command, args []string) {
	status := ctl.Status()
	for _, req := range status.RequestInfo {
		fmt.Println(req)
	}
}

func (ctl *GopCtl) goros(cmd *cobra.Command, args []string) {
	for _, goro := range ctl.Stack().Goros() {
		lines := goro.RoutineLines()
		fmt.Println(goro.Head(), lines[0])
		if ctl.Created && len(lines) > 2 {
			fmt.Println("\t", lines[len(lines)-3])
		}
	}
}

func (ctl *GopCtl) top(cmd *cobra.Command, args []string) {
	for {
		sh := exec.Command("clear") //Linux only
		sh.Stdout = os.Stdout
		sh.Run()

		status := ctl.Status()
		mem := ctl.Mem()

		fmt.Println(status.ProjectName, status.AppName, "\t", ctl.C.AppURL.String(), "\t at", time.Now())
		fmt.Printf("Started at %s up for %fs\n", status.StartTime, status.UptimeSeconds)

		fmt.Printf("\nMEM:\t")
		fmt.Printf("Alloc %dbytes\t", mem.Alloc)
		fmt.Printf("Total %dbytes\t", mem.TotalAlloc)
		fmt.Printf("Sys %dbytes\t", mem.Sys)
		fmt.Printf("Lookups %d\t", mem.Lookups)
		fmt.Printf("Mallocs %d\t", mem.Mallocs)

		fmt.Printf("\nGC:\t")
		fmt.Printf("%d\t", mem.NumGC)
		fmt.Printf("Next %d\t", mem.NextGC)
		fmt.Printf("Last %d\t", mem.LastGC)
		fmt.Printf("PauseTotal %dns\t", mem.PauseTotalNs)
		fmt.Printf("Enable %t\t", mem.EnableGC)
		fmt.Printf("Debug %t\t", mem.DebugGC)
		fmt.Println()

		fmt.Println()
		fmt.Println("Requests:", len(status.RequestInfo))
		for _, req := range status.RequestInfo {
			fmt.Println("\t", req)
		}

		stack := ctl.Stack()
		fmt.Println()
		fmt.Printf("Goros: %d\n", status.NumGoros)
		for _, goro := range stack.Goros() {
			fmt.Println("\t", goro.Head())
		}

		time.Sleep(2 * time.Second)
	}
}

func main() {
	c := NewGopCtl()
	c.Execute()
}

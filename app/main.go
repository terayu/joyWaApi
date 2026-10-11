package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/comail/colog"
	"github.com/terayu/joyWaApi/cmd"
)

var loglevel string

func init() {
	flag.CommandLine.Init("main", flag.ExitOnError)
	flag.StringVar(&loglevel, "loglevel", "info", "log level(trace,debug,info,warning,error,alert,panic)")

	flag.VisitAll(func(f *flag.Flag) {
		if v := os.Getenv("JOYWAAPI_" + strings.ToUpper(f.Name)); v != "" {
			if err := f.Value.Set(v); err != nil {
				panic(err)
			}
		}
	})
}

func main() {

	flag.Parse()

	colog.SetOutput(os.Stderr)
	colog.SetDefaultLevel(colog.LDebug)

	lv, err := colog.ParseLevel(loglevel)
	if err != nil {
		panic(err)
	}

	colog.SetFormatter(&colog.StdFormatter{
		Colors: false,
		Flag:   log.Ldate | log.Ltime | log.Lshortfile,
	})
	colog.SetMinLevel(lv)
	colog.Register()

	fs := make(map[string]func([]string) error)
	fs["server"] = cmd.ServerCmd.Run

	if flag.NArg() > 0 {
		args := flag.Args()

		f, ok := fs[args[0]]
		if !ok {
			log.Panicf("NotFound Function:%v", args[0])
		}
		if err := f(args[1:]); err != nil {
			log.Panicf("err: %v", err)
		}
	}
}

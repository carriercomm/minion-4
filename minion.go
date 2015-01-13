package main

import (
	"flag"
	// "io"
	"log"
	"net/http"
	"os"
	"path"
	"syscall"
	"time"

	eventsource "github.com/antage/eventsource"
	handlers "github.com/gorilla/handlers"
	rpc "github.com/gorilla/rpc/v2"
	json "github.com/gorilla/rpc/v2/json"
	daemon "github.com/sevlyar/go-daemon"
)

var (
	listen     string = "0.0.0.0:9090"
	rootPath   string = currentDir()
	pidFile    string = "log/minion.pid"
	logFile    string = "log/minion.log"
	accessFile string = "log/minion-access.log"
	quiet      bool   = false
	signal     string = ""
)

func checkFile(file string) string {

	var err error = nil

	if !path.IsAbs(file) {
		file = path.Join(rootPath, file)
	}

	_, err = os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			dir := path.Dir(file)
			err = os.MkdirAll(dir, 644)
			if err != nil {
				log.Panic(err)
			}
		} else {
			log.Panic(err)
		}
	}

	return file
}

func main() {

	// error
	var err error

	// parse arguments
	flag.StringVar(&listen, "listen", listen, "Listening address and port for the service.")
	flag.StringVar(&pidFile, "pid", pidFile, "Path to PID file.")
	flag.StringVar(&logFile, "log", logFile, "Path to Log file.")
	flag.StringVar(&accessFile, "access", accessFile, "Path to access log file.")
	flag.StringVar(&rootPath, "root", rootPath, "Path to minion root.")
	flag.BoolVar(&quiet, "quiet", quiet, "If enabled, then do not send output to console.")
	flag.StringVar(&signal, "signal", signal, `send signal to the daemon
		quit — graceful shutdown
		stop — fast shutdown
		reload — reloading the configuration file`)
	flag.Parse()

	// daemon signal handlers
	daemon.AddCommand(daemon.StringFlag(&signal, "quit"), syscall.SIGQUIT, signalTerm)
	daemon.AddCommand(daemon.StringFlag(&signal, "stop"), syscall.SIGTERM, signalTerm)
	daemon.AddCommand(daemon.StringFlag(&signal, "reload"), syscall.SIGHUP, signalHup)

	os.Setenv("GOPATH", rootPath)

	// check files
	pidFile = checkFile(pidFile)
	logFile = checkFile(logFile)
	accessFile = checkFile(accessFile)

	// daemon context
	ctx := &daemon.Context{
		PidFileName: pidFile,
		PidFilePerm: 0644,
		LogFileName: logFile,
		LogFilePerm: 0644,
		WorkDir:     rootPath,
		Umask:       027,
		Args:        []string{},
	}

	if len(daemon.ActiveFlags()) > 0 {
		d, err := ctx.Search()
		if err != nil {
			log.Fatalln("Unable send signal to the daemon:", err)
		}
		daemon.SendCommands(d)
		return
	}

	d, err := ctx.Reborn()
	if err != nil {
		log.Fatalln(err)
	}
	if d != nil {
		return
	}
	defer ctx.Release()

	// daemon handles signals
	if err = daemon.ServeSignals(); err != nil {
		log.Panic(err)
	}

	// open access log
	accessLog, err := os.OpenFile(accessFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Panic("error opening access log: %v", err)
	}
	defer accessLog.Close()

	// server sent events
	eventSource := eventsource.New(nil, nil)
	defer eventSource.Close()

	// services contexts
	serviceContext := &ServiceContext{
		SendEventMessage: eventSource.SendEventMessage,
		Registry:         map[string]string{},
	}

	// export services
	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(json.NewCodec(), "application/json")
	rpcServer.RegisterService(serviceContext, "Service")

	// routes
	httpRouter := http.NewServeMux()
	httpRouter.Handle("/rpc", handlers.CombinedLoggingHandler(accessLog, rpcServer))
	httpRouter.Handle("/events", handlers.CombinedLoggingHandler(accessLog, eventSource))

	// server
	httpServer := &http.Server{
		Addr:           listen,
		Handler:        httpRouter,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// stats polling thread
	go func() {
		params := map[string]interface{}{}
		for {
			select {
			// case m := <-c:
			// 	handle(m)
			case <-time.After(time.Second):
				for name, _ := range serviceContext.Registry {
					var res string
					err := serviceContext.run(name, "stats", params, &res)
					if err != nil {
						println(err.Error())
					} else {
						serviceContext.SendEventMessage(res, "stats:"+name, "")
					}
				}
			}
		}
	}()

	// start
	go func() {
		log.Printf("Starting HTTP on http://%s\n", listen)
		log.Panic(httpServer.ListenAndServe())
	}()

	// exit handled by signal handlers
	halt := make(chan bool)
	<-halt
}

func currentDir() string {
	s, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return s
}

func signalQuit(s os.Signal) error {
	// logInfo("Signal QUIT Received %v", sig)
	os.Exit(0)
	return nil
}

func signalTerm(s os.Signal) error {
	// logInfo("Signal TERM Received %v", sig)
	os.Exit(0)
	return nil
}

func signalHup(s os.Signal) error {
	// logInfo("Signal HUP Received %v", s)
	return nil
}

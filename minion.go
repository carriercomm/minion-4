package main

import (
	"flag"
	"io"
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
	errorPath  string = "log/error.log"
	accessPath string = "log/access.log"
	quiet      bool   = false
	signal     string = ""
)

func main() {

	// error
	var err error

	// parse arguments
	flag.StringVar(&listen, "listen", listen, "Listening address and port for the service")
	flag.StringVar(&rootPath, "root", rootPath, "Path to minion root")
	flag.StringVar(&errorPath, "error", errorPath, "Path to error log")
	flag.StringVar(&accessPath, "access", accessPath, "Path to access log")
	flag.BoolVar(&quiet, "quiet", quiet, "If enabled, then do not send output to console.")
	flag.StringVar(&signal, "signal", signal, `send signal to the daemon
		quit — graceful shutdown
		stop — fast shutdown
		reload — reloading the configuration file`)
	flag.Parse()

	os.Setenv("GOPATH", rootPath)

	// ensure path variables are absolute paths
	if !path.IsAbs(errorPath) {
		errorPath = path.Join(rootPath, errorPath)
	}
	if !path.IsAbs(accessPath) {
		accessPath = path.Join(rootPath, accessPath)
	}

	// check the errorPath
	_, err = os.Stat(errorPath)
	if err != nil {
		if os.IsNotExist(err) {
			dir := path.Dir(errorPath)
			err = os.MkdirAll(dir, 755)
			if err != nil {
				log.Panic(err)
			}
		} else {
			log.Panic(err)
		}
	}

	// check the accessPath
	_, err = os.Stat(accessPath)
	if err != nil {
		if os.IsNotExist(err) {
			dir := path.Dir(accessPath)
			err = os.MkdirAll(dir, 755)
			if err != nil {
				log.Panic(err)
			}
		} else {
			log.Panic(err)
		}
	}

	// open access log
	accessLog, err := os.OpenFile(accessPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panic("error opening access log: %v", err)
	}
	defer accessLog.Close()

	// open error log
	errorLog, err := os.OpenFile(errorPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panic("error opening error log: %v", err)
	}
	defer errorLog.Close()

	// set log to error log
	if quiet {
		log.SetOutput(errorLog)
	} else {
		log.SetOutput(io.MultiWriter(os.Stdout, errorLog))
	}

	// daemon signal handlers
	daemon.AddCommand(daemon.StringFlag(&signal, "quit"), syscall.SIGQUIT, signalTerm)
	daemon.AddCommand(daemon.StringFlag(&signal, "stop"), syscall.SIGTERM, signalTerm)
	daemon.AddCommand(daemon.StringFlag(&signal, "reload"), syscall.SIGHUP, signalHup)

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
	// go func() {
	// }()

	// daemon handles signals
	if err = daemon.ServeSignals(); err != nil {
		log.Panic(err)
	}

	log.Printf("Starting HTTP on http://%s\n", listen)
	log.Panic(httpServer.ListenAndServe())

	// exit handled by signal handlers
	// halt := make(chan bool)
	// <-halt
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

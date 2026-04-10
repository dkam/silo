// Main package for Seafile file server.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/haiwen/seafile-server/fileserver/api"
	"github.com/haiwen/seafile-server/fileserver/authmgr"
	"github.com/haiwen/seafile-server/fileserver/blockmgr"
	"github.com/haiwen/seafile-server/fileserver/commitmgr"
	"github.com/haiwen/seafile-server/fileserver/dbutil"
	"github.com/haiwen/seafile-server/fileserver/fsmgr"
	"github.com/haiwen/seafile-server/fileserver/keycache"
	"github.com/haiwen/seafile-server/fileserver/metrics"
	"github.com/haiwen/seafile-server/fileserver/middleware"
	"github.com/haiwen/seafile-server/fileserver/option"
	"github.com/haiwen/seafile-server/fileserver/repomgr"
	"github.com/haiwen/seafile-server/fileserver/share"
	"github.com/haiwen/seafile-server/fileserver/tokenstore"
	"github.com/haiwen/seafile-server/fileserver/utils"
	log "github.com/sirupsen/logrus"

	"net/http/pprof"
)

var dataDir, absDataDir string
var centralDir string
var logFile, absLogFile string
var pidFilePath string
var logFp *os.File

var seafilePair, ccnetPair *dbutil.DBPair

var logToStdout bool

func init() {
	flag.StringVar(&centralDir, "F", "", "central config directory")
	flag.StringVar(&dataDir, "d", "", "seafile data directory")
	flag.StringVar(&logFile, "l", "", "log file path")
	flag.StringVar(&pidFilePath, "P", "", "pid file path")

	env := os.Getenv("SEAFILE_LOG_TO_STDOUT")
	if env == "true" {
		logToStdout = true
	}

	log.SetFormatter(&LogFormatter{})
}

const (
	timestampFormat = "[2006-01-02 15:04:05] "
)

type LogFormatter struct{}

func (f *LogFormatter) Format(entry *log.Entry) ([]byte, error) {
	levelStr := entry.Level.String()
	if levelStr == "fatal" {
		levelStr = "ERROR"
	} else {
		levelStr = strings.ToUpper(levelStr)
	}
	level := fmt.Sprintf("[%s] ", levelStr)
	appName := ""
	if logToStdout {
		appName = "[fileserver] "
	}
	buf := make([]byte, 0, len(appName)+len(timestampFormat)+len(level)+len(entry.Message)+1)
	if logToStdout {
		buf = append(buf, appName...)
	}
	buf = entry.Time.AppendFormat(buf, timestampFormat)
	buf = append(buf, level...)
	buf = append(buf, entry.Message...)
	buf = append(buf, '\n')
	return buf, nil
}

func loadDatabases() {
	dbOpt, err := option.LoadDBOption(centralDir)
	if err != nil {
		log.Fatalf("Failed to load database: %v", err)
	}

	dbutil.DBEngine = dbOpt.DBEngine
	if dbOpt.DBEngine == dbutil.EngineSQLite {
		loadSQLiteDatabases()
	} else {
		loadMySQLDatabases(dbOpt)
	}
}

func loadSQLiteDatabases() {
	ccnetPath := filepath.Join(absDataDir, "ccnet.db")
	seafilePath := filepath.Join(absDataDir, "seafile.db")

	var err error
	ccnetPair, err = dbutil.OpenSQLite(ccnetPath)
	if err != nil {
		log.Fatalf("Failed to open ccnet database: %v", err)
	}

	if err := dbutil.CreateCcnetTables(ccnetPair.Write); err != nil {
		log.Fatalf("Failed to create ccnet tables: %v", err)
	}

	seafilePair, err = dbutil.OpenSQLite(seafilePath)
	if err != nil {
		log.Fatalf("Failed to open seafile database: %v", err)
	}

	if err := dbutil.CreateSeafileTables(seafilePair.Write); err != nil {
		log.Fatalf("Failed to create seafile tables: %v", err)
	}

	log.Info("Using SQLite databases")
}

func loadMySQLDatabases(dbOpt *option.DBOption) {
	ccnetDSN := buildMySQLDSN(dbOpt, dbOpt.CcnetDbName)
	seafileDSN := buildMySQLDSN(dbOpt, dbOpt.SeafileDbName)

	var err error
	ccnetPair, err = dbutil.OpenMySQL(ccnetDSN)
	if err != nil {
		log.Fatalf("Failed to open ccnet database: %v", err)
	}

	seafilePair, err = dbutil.OpenMySQL(seafileDSN)
	if err != nil {
		log.Fatalf("Failed to open seafile database: %v", err)
	}

	log.Info("Using MySQL databases")
}

func buildMySQLDSN(dbOpt *option.DBOption, dbName string) string {
	timeout := "&readTimeout=60s" + "&writeTimeout=60s"
	var dsn string
	if dbOpt.UseTLS && dbOpt.SkipVerify {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?tls=skip-verify%s", dbOpt.User, dbOpt.Password, dbOpt.Host, dbOpt.Port, dbName, timeout)
	} else if dbOpt.UseTLS && !dbOpt.SkipVerify {
		registerCA(dbOpt.CaPath)
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?tls=custom%s", dbOpt.User, dbOpt.Password, dbOpt.Host, dbOpt.Port, dbName, timeout)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?tls=%t%s", dbOpt.User, dbOpt.Password, dbOpt.Host, dbOpt.Port, dbName, dbOpt.UseTLS, timeout)
	}
	if dbOpt.Charset != "" {
		dsn = fmt.Sprintf("%s&charset=%s", dsn, dbOpt.Charset)
	}
	return dsn
}

// registerCA registers CA to verify server cert.
func registerCA(capath string) {
	rootCertPool := x509.NewCertPool()
	pem, err := os.ReadFile(capath)
	if err != nil {
		log.Fatal(err)
	}
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		log.Fatal("Failed to append PEM.")
	}
	mysql.RegisterTLSConfig("custom", &tls.Config{
		RootCAs: rootCertPool,
	})
}

func writePidFile(pid_file_path string) error {
	file, err := os.OpenFile(pid_file_path, os.O_CREATE|os.O_WRONLY, 0664)
	if err != nil {
		return err
	}
	defer file.Close()

	pid := os.Getpid()
	str := fmt.Sprintf("%d", pid)
	_, err = file.Write([]byte(str))

	if err != nil {
		return err
	}
	return nil
}

func removePidfile(pid_file_path string) error {
	err := os.Remove(pid_file_path)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	flag.Parse()

	if centralDir == "" {
		log.Fatal("central config directory must be specified.")
	}

	if pidFilePath != "" {
		if writePidFile(pidFilePath) != nil {
			log.Fatal("write pid file failed.")
		}
	}
	_, err := os.Stat(centralDir)
	if os.IsNotExist(err) {
		log.Fatalf("central config directory %s doesn't exist: %v.", centralDir, err)
	}

	if dataDir == "" {
		log.Fatal("seafile data directory must be specified.")
	}
	_, err = os.Stat(dataDir)
	if os.IsNotExist(err) {
		log.Fatalf("seafile data directory %s doesn't exist: %v.", dataDir, err)
	}
	absDataDir, err = filepath.Abs(dataDir)
	if err != nil {
		log.Fatalf("Failed to convert seafile data dir to absolute path: %v.", err)
	}

	if logToStdout {
		// Use default output (StdOut)
	} else if logFile == "" {
		absLogFile = filepath.Join(absDataDir, "fileserver.log")
		fp, err := os.OpenFile(absLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to open or create log file: %v", err)
		}
		logFp = fp
		log.SetOutput(fp)
	} else if logFile != "-" {
		absLogFile, err = filepath.Abs(logFile)
		if err != nil {
			log.Fatalf("Failed to convert log file path to absolute path: %v", err)
		}
		fp, err := os.OpenFile(absLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to open or create log file: %v", err)
		}
		logFp = fp
		log.SetOutput(fp)
	}

	if absLogFile != "" && !logToStdout {
		utils.Dup(int(logFp.Fd()), int(os.Stderr.Fd()))
	}
	// When logFile is "-", use default output (StdOut)

	if err := option.LoadSeahubConfig(); err != nil {
		log.Fatalf("Failed to read seahub config: %v", err)
	}

	option.LoadFileServerOptions(centralDir)
	loadDatabases()

	level, err := log.ParseLevel(option.LogLevel)
	if err != nil {
		log.Info("use the default log level: info")
		log.SetLevel(log.InfoLevel)
	} else {
		log.SetLevel(level)
	}

	repomgr.Init(seafilePair.Read, seafilePair.Write)

	fsmgr.Init(centralDir, dataDir, option.FsCacheLimit)

	blockmgr.Init(centralDir, dataDir)

	commitmgr.Init(centralDir, dataDir)

	share.Init(ccnetPair.Read, seafilePair.Read, option.GroupTableName, option.CloudMode)

	tokenstore.StartCleanup()
	keycache.StartReaper()
	authmgr.Init(ccnetPair.Read, ccnetPair.Write)
	api.Init(seafilePair.Read, seafilePair.Write)

	// Create admin user from env vars if set
	adminEmail := os.Getenv("SEAFILE_ADMIN_EMAIL")
	adminPassword := os.Getenv("SEAFILE_ADMIN_PASSWORD")
	if adminEmail != "" && adminPassword != "" {
		if err := authmgr.EnsureAdmin(adminEmail, adminPassword); err != nil {
			log.Fatalf("Failed to create admin user: %v", err)
		}
	}

	fileopInit()

	syncAPIInit()

	sizeSchedulerInit()

	virtualRepoInit()

	initUpload()

	metrics.Init()

	router := newHTTPRouter()

	go handleSignals()
	go handleUser1Signal()

	log.Print("Seafile file server started.")

	server := new(http.Server)
	server.Addr = fmt.Sprintf("%s:%d", option.Host, option.Port)
	server.Handler = router

	err = server.ListenAndServe()
	if err != nil {
		log.Errorf("File server exiting: %v", err)
	}
}

func handleSignals() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-signalChan
	metrics.Stop()
	removePidfile(pidFilePath)
	os.Exit(0)
}

func handleUser1Signal() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGUSR1)

	for {
		<-signalChan
		logRotate()
	}
}

func logRotate() {
	if logToStdout {
		return
	}
	// reopen fileserver log
	fp, err := os.OpenFile(absLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("Failed to reopen fileserver log: %v", err)
	}
	log.SetOutput(fp)
	if logFp != nil {
		logFp.Close()
		logFp = fp
	}

	utils.Dup(int(logFp.Fd()), int(os.Stderr.Fd()))
}

func newHTTPRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/protocol-version{slash:\\/?}", handleProtocolVersion)
	r.Handle("/files/{.*}/{.*}", appHandler(accessCB))
	r.Handle("/blks/{.*}/{.*}", appHandler(accessBlksCB))
	r.Handle("/zip/{.*}", appHandler(accessZipCB))
	r.Handle("/upload-api/{.*}", appHandler(uploadAPICB))
	r.Handle("/upload-aj/{.*}", appHandler(uploadAjaxCB))
	r.Handle("/update-api/{.*}", appHandler(updateAPICB))
	r.Handle("/update-aj/{.*}", appHandler(updateAjaxCB))
	r.Handle("/upload-blks-api/{.*}", appHandler(uploadBlksAPICB))
	r.Handle("/upload-raw-blks-api/{.*}", appHandler(uploadRawBlksAPICB))

	// links api
	//r.Handle("/u/{.*}", appHandler(uploadLinkCB))
	r.Handle("/f/{.*}{slash:\\/?}", appHandler(accessLinkCB))
	//r.Handle("/d/{.*}", appHandler(accessDirLinkCB))

	r.Handle("/repos/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/files/{filepath:.*}", appHandler(accessV2CB))

	// file syncing api
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/permission-check{slash:\\/?}",
		appHandler(permissionCheckCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/commit/HEAD{slash:\\/?}",
		appHandler(headCommitOperCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/commit/{id:[\\da-z]{40}}",
		appHandler(commitOperCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/block/{id:[\\da-z]{40}}",
		appHandler(blockOperCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/fs-id-list{slash:\\/?}",
		appHandler(getFsObjIDCB))
	r.Handle("/repo/head-commits-multi{slash:\\/?}",
		appHandler(headCommitsMultiCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/pack-fs{slash:\\/?}",
		appHandler(packFSCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/check-fs{slash:\\/?}",
		appHandler(checkFSCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/check-blocks{slash:\\/?}",
		appHandler(checkBlockCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/recv-fs{slash:\\/?}",
		appHandler(recvFSCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/quota-check{slash:\\/?}",
		appHandler(getCheckQuotaCB))
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/jwt-token{slash:\\/?}",
		appHandler(getJWTTokenCB))

	// seadrive api
	r.Handle("/repo/{repoid:[\\da-z]{8}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{4}-[\\da-z]{12}}/block-map/{id:[\\da-z]{40}}",
		appHandler(getBlockMapCB))
	r.Handle("/accessible-repos{slash:\\/?}", appHandler(getAccessibleRepoListCB))

	// pprof
	r.Handle("/debug/pprof", &profileHandler{http.HandlerFunc(pprof.Index)})
	r.Handle("/debug/pprof/cmdline", &profileHandler{http.HandlerFunc(pprof.Cmdline)})
	r.Handle("/debug/pprof/profile", &profileHandler{http.HandlerFunc(pprof.Profile)})
	r.Handle("/debug/pprof/symbol", &profileHandler{http.HandlerFunc(pprof.Symbol)})
	r.Handle("/debug/pprof/heap", &profileHandler{pprof.Handler("heap")})
	r.Handle("/debug/pprof/block", &profileHandler{pprof.Handler("block")})
	r.Handle("/debug/pprof/goroutine", &profileHandler{pprof.Handler("goroutine")})
	r.Handle("/debug/pprof/threadcreate", &profileHandler{pprof.Handler("threadcreate")})
	r.Handle("/debug/pprof/trace", &traceHandler{})

	// Management API
	r.HandleFunc("/api/v1/auth/login", api.LoginHandler).Methods("POST")
	apiRouter := r.PathPrefix("/api/v1").Subrouter()
	apiRouter.Use(middleware.RequireAuth)
	apiRouter.HandleFunc("/access-tokens", api.CreateAccessTokenHandler).Methods("POST")
	apiRouter.HandleFunc("/repos", api.ListReposHandler).Methods("GET")
	apiRouter.HandleFunc("/repos", api.CreateRepoHandler).Methods("POST")
	apiRouter.HandleFunc("/repos/{repoid}", api.DeleteRepoHandler).Methods("DELETE")
	apiRouter.HandleFunc("/repos/{repoid}/dir/", api.ListDirHandler).Methods("GET")
	apiRouter.HandleFunc("/repos/{repoid}/mkdir", mkdirHandler).Methods("POST")
	apiRouter.HandleFunc("/repos/{repoid}/file", deleteFileHandler).Methods("DELETE")
	apiRouter.HandleFunc("/repos/{repoid}/download", downloadFileHandler).Methods("GET")
	apiRouter.HandleFunc("/repos/{repoid}/sync-token", api.CreateRepoSyncTokenHandler).Methods("POST")

	if option.HasRedisOptions {
		r.Use(metrics.MetricMiddleware)
	}
	return r
}

func handleProtocolVersion(rsp http.ResponseWriter, r *http.Request) {
	io.WriteString(rsp, "{\"version\": 2}")
}

type appError struct {
	Error   error
	Message string
	Code    int
}

type appHandler func(http.ResponseWriter, *http.Request) *appError

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := fn(w, r); e != nil {
		if e.Error != nil && e.Code == http.StatusInternalServerError {
			log.Errorf("path %s internal server error: %v\n", r.URL.Path, e.Error)
		}
		http.Error(w, e.Message, e.Code)
	}
}

func RecoverWrapper(f func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("panic: %v\n%s", err, debug.Stack())
		}
	}()

	f()
}

type profileHandler struct {
	pHandler http.Handler
}

func (p *profileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	queries := r.URL.Query()
	password := queries.Get("password")
	if !option.EnableProfiling || password != option.ProfilePassword {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	p.pHandler.ServeHTTP(w, r)
}

type traceHandler struct {
}

func (p *traceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	queries := r.URL.Query()
	password := queries.Get("password")
	if !option.EnableProfiling || password != option.ProfilePassword {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	pprof.Trace(w, r)
}

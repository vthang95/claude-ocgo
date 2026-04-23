package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/logger"
	"github.com/vthang95/claude-ocgo/routes"
)

const usage = `ocgo - OpenCode Go proxy for Claude

Usage:
  ocgo <command> [flags]

Commands:
  run     Start the proxy server
  stop    Stop daemon
  status  Show daemon status
  logs    Tail daemon logs (--verbose for full details)

Run flags:
  -p, --port           <port>   Listen port (default: 14242, env: PORT)
  -u, --upstream       <url>    Upstream base URL (env: OPENCODE_API_URL)
  -m, --model          <model>  Default model (default: qwen3.6-plus, env: DEFAULT_MODEL)
  -wf, --with-fallback            Enable automatic fallback to alternative models on failure
  -om, --overwrite-model          Always use default model, ignore Claude's model setting
  -d, --daemon                    Run server in background (daemon mode)

API key is read from OPENCODE_API_KEY environment variable.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runServer(os.Args[2:])
	case "stop":
		stopServer()
	case "status":
		statusServer()
	case "logs":
		logsServer(os.Args[2:])
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}

func runServer(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	port := fs.String("port", config.PORT, "")
	portShort := fs.String("p", config.PORT, "")
	upstream := fs.String("upstream", config.UPSTREAM_BASE, "")
	upstreamShort := fs.String("u", config.UPSTREAM_BASE, "")
	model := fs.String("model", config.DEFAULT_MODEL, "")
	modelShort := fs.String("m", config.DEFAULT_MODEL, "")
	withFallback := fs.Bool("with-fallback", false, "")
	withFallbackShort := fs.Bool("wf", false, "")
	overwriteModel := fs.Bool("overwrite-model", false, "")
	overwriteModelShort := fs.Bool("om", false, "")
	daemon := fs.Bool("daemon", false, "")
	daemonShort := fs.Bool("d", false, "")
	fs.Usage = func() { fmt.Print(usage) }
	fs.Parse(args)

	if *portShort != config.PORT {
		*port = *portShort
	}
	if *modelShort != config.DEFAULT_MODEL {
		*model = *modelShort
	}
	if *upstreamShort != config.UPSTREAM_BASE {
		*upstream = *upstreamShort
	}
	if *withFallbackShort {
		*withFallback = true
	}
	if *overwriteModelShort {
		*overwriteModel = true
	}
	if *daemonShort {
		*daemon = true
	}

	// Daemon mode: re-exec and let parent exit
	if *daemon {
		daemonize(*port)
		return
	}

	config.PORT = *port
	config.UPSTREAM_BASE = *upstream
	config.DEFAULT_MODEL = *model

	if *withFallback {
		config.WITH_FALLBACK = true
	}
	if *overwriteModel {
		config.OVERWRITE_MODEL = true
	}

	startServer()
}

// daemonize forks a background process with the same flags minus --daemon
func daemonize(port string) {
	pidFile := getPidFilePath()

	// Check if already running
	if pid, err := readPidFile(pidFile); err == nil {
		if processExists(pid) {
			fmt.Printf("ocgo is already running (pid: %d)\n", pid)
			return
		}
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot find executable: %v\n", err)
		os.Exit(1)
	}

	// Build args without daemon flags
	args := []string{"run"}
	for _, a := range os.Args[2:] {
		if a == "--daemon" || a == "-d" {
			continue
		}
		args = append(args, a)
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()

	os.MkdirAll(config.LogDir(), 0755)
	stdoutFile, err := os.OpenFile(logPath(port), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open log file: %v\n", err)
		os.Exit(1)
	}
	defer stdoutFile.Close()

	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to start daemon: %v\n", err)
		os.Exit(1)
	}

	writePidFile(pidFile, cmd.Process.Pid)

	fmt.Printf("ocgo started in background (pid: %d, log: %s)\n", cmd.Process.Pid, logPath(port))
	fmt.Printf("Run 'ocgo stop' to shut down.\n")
}

func startServer() {
	logger.Init()
	defer logger.Close()

	mux := http.NewServeMux()

	handler := corsMiddleware(mux)
	handler = loggingMiddleware(handler)
	handler = recoveryMiddleware(handler)

	routes.RegisterHealth(mux)
	routes.RegisterMessages(mux)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "not_found",
				"message": fmt.Sprintf("%s %s not found", r.Method, r.URL.Path),
			},
		})
	})

	addr := ":" + config.PORT
	fmt.Printf("Proxy listening on http://127.0.0.1%s\n", addr)
	fmt.Printf("Upstream: %s\n", config.UPSTREAM_BASE)
	if config.UPSTREAM_KEY != "" {
		fmt.Println("API key: set")
	} else {
		fmt.Println("API key: NOT SET")
	}
	fmt.Printf("Default model: %s\n", config.DEFAULT_MODEL)
	if config.WITH_FALLBACK {
		fmt.Printf("Fallback models: %v\n", config.FallbackModels)
	}
	if config.OVERWRITE_MODEL {
		fmt.Println("Overwrite model: enabled")
	}
	fmt.Printf("Config dir: %s\n", config.ConfigDir())
	fmt.Printf("Log dir: %s\n", config.LogDir())

	logger.WriteEvent("SERVER_START", map[string]interface{}{
		"addr":  addr,
		"upstream": config.UPSTREAM_BASE,
		"model": config.DEFAULT_MODEL,
		"fallback": config.WITH_FALLBACK,
		"overwriteModel": config.OVERWRITE_MODEL,
	})

	server := &http.Server{Addr: addr, Handler: handler}

	// Graceful shutdown on SIGTERM / SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logger.WriteEvent("SERVER_STOP", map[string]interface{}{
			"signal": sig.String(),
		})
		logger.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.WriteEvent("SERVER_ERROR", map[string]interface{}{
			"error": err.Error(),
		})
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, anthropic-version, x-api-key")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware catches panics, logs them to the log file, and returns 500.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.WriteEvent("SERVER_CRASH", map[string]interface{}{
					"method": r.Method,
					"path":   r.URL.Path,
					"panic":  fmt.Sprintf("%v", err),
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"type":    "internal_error",
						"message": "internal server error",
					},
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		ms := time.Since(start).Milliseconds()
		model := wrapped.model
		if model == "" {
			model = "-"
		}
		line := fmt.Sprintf("%s [%s] %s → %d (%dms)",
			r.Method, model, r.URL.Path, wrapped.statusCode, ms)
		fmt.Printf("[%s] %s\n", time.Now().UTC().Format(time.RFC3339), line)
		logger.WriteRequest(line)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	model      string
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) SetModel(m string) {
	rw.model = m
}

// --- daemon helpers ---

func getPidFilePath() string {
	return filepath.Join(config.ConfigDir(), "ocgo.pid")
}

func logPath(port string) string {
	return filepath.Join(config.LogDir(), fmt.Sprintf("ocgo-%s.log", port))
}

func writePidFile(path string, pid int) {
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func readPidFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	return pid, nil
}

func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func stopServer() {
	pidFile := getPidFilePath()
	pid, err := readPidFile(pidFile)
	if err != nil {
		fmt.Println("ocgo is not running (no pid file found)")
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("ocgo process (pid: %d) not found\n", pid)
		os.Remove(pidFile)
		return
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("ocgo process (pid: %d) already stopped\n", pid)
		os.Remove(pidFile)
		return
	}

	os.Remove(pidFile)
	fmt.Printf("ocgo stopped (pid: %d)\n", pid)

	// Also write stop event to the log file so it appears in 'ocgo logs'.
	logDir := config.LogDir()
	today := time.Now().Format("2006-01-02")
	lf, err := os.OpenFile(filepath.Join(logDir, fmt.Sprintf("proxy-%s.log", today)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		ts := time.Now().UTC().Format(time.RFC3339)
		evt := fmt.Sprintf("%s [EVENT] SERVER_STOP {\"source\":\"cli\",\"pid\":%d}\n", ts, pid)
		lf.WriteString(evt)
		lf.Close()
	}
}

func statusServer() {
	pidFile := getPidFilePath()
	pid, err := readPidFile(pidFile)
	if err != nil {
		fmt.Println("ocgo is not running")
		return
	}

	if processExists(pid) {
		fmt.Printf("ocgo is running (pid: %d)\n", pid)
	} else {
		fmt.Printf("stale pid file found, process %d not running\n", pid)
		os.Remove(pidFile)
	}
}

func logsServer(args []string) {
	verbose := false
	for _, a := range args {
		if a == "--verbose" || a == "-v" {
			verbose = true
		}
	}

	logDir := config.LogDir()
	os.MkdirAll(logDir, 0755)
	today := time.Now().UTC().Format("2006-01-02")

	var files []*os.File

	f, err := os.Open(filepath.Join(logDir, fmt.Sprintf("proxy-%s.log", today)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open log file: %v\n", err)
		return
	}
	files = append(files, f)
	defer f.Close()

	if verbose {
		vf, err := os.Open(filepath.Join(logDir, fmt.Sprintf("proxy-%s-verbose.log", today)))
		if err == nil {
			files = append(files, vf)
			defer vf.Close()
		}
	}

	// Print existing content from all files
	pos := make([]int64, len(files))
	for i, ff := range files {
		scanner := bufio.NewScanner(ff)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if verbose && i == 0 {
			fmt.Println("--- verbose logs ---")
		}
		pos[i], _ = ff.Seek(0, 1)
	}

	// Tail all files
	for {
		anyNew := false
		for i, ff := range files {
			scanner := bufio.NewScanner(ff)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
				anyNew = true
			}
			newPos, _ := ff.Seek(0, 1)
			pos[i] = newPos
		}
		if !anyNew {
			time.Sleep(100 * time.Millisecond)
		}
	}
}


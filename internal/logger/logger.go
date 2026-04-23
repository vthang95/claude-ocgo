package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vthang95/claude-ocgo/internal/config"
)

var (
	reqLog    *os.File
	verboseLog *os.File
	logMu     sync.Mutex
)

func Init() {
	logDir := config.LogDir()
	os.MkdirAll(logDir, 0755)
	today := time.Now().Format("2006-01-02")

	f, err := os.OpenFile(filepath.Join(logDir, fmt.Sprintf("proxy-%s.log", today)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open request log: %v\n", err)
		return
	}
	reqLog = f

	v, err := os.OpenFile(filepath.Join(logDir, fmt.Sprintf("proxy-%s-verbose.log", today)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open verbose log: %v\n", err)
		return
	}
	verboseLog = v
}

func WriteLog(tag string, data interface{}) {
	writeVerboseEntry(tag, data)
	fmt.Printf("[LOG] %s\n", tag)
}

func WriteLogVerbose(tag string, data interface{}) {
	b, _ := json.Marshal(data)
	writeVerboseEntry(tag, data)
	fmt.Printf("[LOG] %s (%d chars)\n", tag, len(b))
}

// WriteRequest appends a plain request line to the request log.
func WriteRequest(line string) {
	if reqLog == nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	reqLog.WriteString(time.Now().UTC().Format(time.RFC3339))
	reqLog.WriteString(" ")
	reqLog.WriteString(line)
	reqLog.WriteString("\n")
}

// WriteEvent logs a lifecycle event (start, stop, crash) to both the
// verbose log and the request log so it shows up in "ocgo logs".
func WriteEvent(tag string, data interface{}) {
	writeVerboseEntry(tag, data)
	if reqLog == nil {
		return
	}
	b, _ := json.Marshal(data)
	logMu.Lock()
	defer logMu.Unlock()
	reqLog.WriteString(time.Now().UTC().Format(time.RFC3339))
	reqLog.WriteString(" [EVENT] ")
	reqLog.WriteString(tag)
	if len(b) > 0 {
		reqLog.WriteString(" ")
		reqLog.Write(b)
	}
	reqLog.WriteString("\n")
}

func Close() {
	if reqLog != nil {
		reqLog.Close()
	}
	if verboseLog != nil {
		verboseLog.Close()
	}
}

func writeVerboseEntry(tag string, data interface{}) {
	if verboseLog == nil {
		return
	}
	entry := map[string]interface{}{
		"ts":   time.Now().UTC().Format(time.RFC3339),
		"tag":  tag,
		"data": data,
	}
	b, _ := json.Marshal(entry)
	logMu.Lock()
	defer logMu.Unlock()
	verboseLog.Write(b)
	verboseLog.Write([]byte("\n"))
}

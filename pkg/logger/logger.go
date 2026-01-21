package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Level represents log levels
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger structure
type Logger struct {
	Level   Level
	Writer  io.Writer
	Prefix  string
	Service string
}

var globalLogger *Logger

// Init initializes the global logger
func Init(logPath string, level Level, serviceName string) error {
	// Create logs directory if it doesn't exist
	logDir := filepath.Dir(logPath)
	if logDir != "." {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// If we can't create the directory, log to stdout only
			fmt.Fprintf(os.Stderr, "Warning: Failed to create log directory %s: %v\n", logDir, err)
			fmt.Fprintf(os.Stderr, "Logging to stdout only\n")
			globalLogger = &Logger{
				Level:   level,
				Writer:  os.Stdout,
				Service: serviceName,
			}
			return nil
		}
	}

	// Open log file
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// If we can't open the file, log to stdout only
		fmt.Fprintf(os.Stderr, "Warning: Failed to open log file %s: %v\n", logPath, err)
		fmt.Fprintf(os.Stderr, "Logging to stdout only\n")
		globalLogger = &Logger{
			Level:   level,
			Writer:  os.Stdout,
			Service: serviceName,
		}
		return nil
	}

	// Only log to file - don't pollute the UI with log messages
	// Events should be displayed via the event system, not logs
	globalLogger = &Logger{
		Level:   level,
		Writer:  file,
		Service: serviceName,
	}

	return nil
}

// Log represents the core logging method
func (l *Logger) log(level Level, scope string, msg string, ctx map[string]interface{}) {
	if level < l.Level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	caller := "unknown:0"
	if ok {
		// Use relative path from project root if possible
		if root, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(root, file); err == nil {
				caller = fmt.Sprintf("%s:%d", rel, line)
			} else {
				caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
			}
		} else {
			caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}
	}

	// Format: [Timestamp] [LEVEL] [Scope] [File:Line] Message JSON
	levelStr := fmt.Sprintf("[%s]", level.String())
	timeStr := fmt.Sprintf("[%s]", timestamp)
	scopeStr := fmt.Sprintf("[%s]", scope)
	callerStr := fmt.Sprintf("[%s]", caller)

	// Merge service into ctx
	if l.Service != "" {
		if ctx == nil {
			ctx = make(map[string]interface{})
		}
		ctx["service"] = l.Service
	}

	jsonCtx := ""
	if len(ctx) > 0 {
		data, _ := json.Marshal(ctx)
		jsonCtx = string(data)
	}

	lineOut := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", timeStr, levelStr, scopeStr, callerStr, msg)
	if jsonCtx != "" {
		lineOut += "\t" + jsonCtx
	}
	lineOut += "\n"

	fmt.Fprint(l.Writer, lineOut)
}

// Global functions
func Info(scope string, msg string, args ...map[string]interface{}) {
	if globalLogger == nil {
		return
	}
	ctx := getCtx(args)
	globalLogger.log(INFO, scope, msg, ctx)
}

func Error(scope string, msg string, args ...map[string]interface{}) {
	if globalLogger == nil {
		return
	}
	ctx := getCtx(args)
	globalLogger.log(ERROR, scope, msg, ctx)
}

func Debug(scope string, msg string, args ...map[string]interface{}) {
	if globalLogger == nil {
		return
	}
	ctx := getCtx(args)
	globalLogger.log(DEBUG, scope, msg, ctx)
}

func Warn(scope string, msg string, args ...map[string]interface{}) {
	if globalLogger == nil {
		return
	}
	ctx := getCtx(args)
	globalLogger.log(WARN, scope, msg, ctx)
}

func getCtx(args []map[string]interface{}) map[string]interface{} {
	if len(args) > 0 {
		return args[0]
	}
	return nil
}

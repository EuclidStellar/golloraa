package utils

import (
    "fmt"
    "io"
    "log"
    "os"
    "path/filepath"
    "runtime"
   // "strings"
    "time"
)

var (
    Info  *log.Logger
    Warn  *log.Logger
    Error *log.Logger
    Debug *log.Logger

)

func InitLogger(infoHandle, warnHandle, errorHandle, debugHandle io.Writer) {
    Info = log.New(infoHandle, "INFO: ", log.Ldate|log.Ltime)
    Warn = log.New(warnHandle, "WARN: ", log.Ldate|log.Ltime)
    Error = log.New(errorHandle, "ERROR: ", log.Ldate|log.Ltime)
    Debug = log.New(debugHandle, "DEBUG: ", log.Ldate|log.Ltime)
}

func SetupLogFile(logDir string) (*os.File, error) {
    if err := os.MkdirAll(logDir, 0755); err != nil {
        return nil, fmt.Errorf("failed to create log directory: %v", err)
    }
    
    logFile := filepath.Join(logDir, fmt.Sprintf("gollora-%s.log", time.Now().Format("2006-01-02")))
    file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    if err != nil {
        return nil, fmt.Errorf("failed to open log file: %v", err)
    }

    infoWriter := io.MultiWriter(file, os.Stdout)
    warnWriter := io.MultiWriter(file, os.Stdout)
    errorWriter := io.MultiWriter(file, os.Stderr)
    
    var debugWriter io.Writer
    if os.Getenv("DEBUG") == "true" {
        debugWriter = io.MultiWriter(file, os.Stdout)
    } else {
        debugWriter = io.MultiWriter(file, io.Discard)
    }
    
    InitLogger(infoWriter, warnWriter, errorWriter, debugWriter)
    
    return file, nil
}

func LogWithLocation(logger *log.Logger, format string, v ...interface{}) {
    _, file, line, ok := runtime.Caller(2)
    if ok {
        file = filepath.Base(file)
        fileInfo := fmt.Sprintf("%s:%d", file, line)
        logger.Printf(fmt.Sprintf("[%s] %s", fileInfo, format), v...)
    } else {
        logger.Printf(format, v...)
    }
}
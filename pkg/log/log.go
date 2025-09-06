package log

import (
	"fmt"
	"time"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorBlue   = "\033[34m"
	ColorYellow = "\033[33m"
	ColorWhite  = "\033[37m"
)

func timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05.000")
}

func Info(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	fmt.Printf("[%s] %s%s%s\n", timestamp(), ColorWhite, formatted, ColorReset)
}

func Wait(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...) + "..."
	fmt.Printf("[%s] %s%s%s\n", timestamp(), ColorBlue, formatted, ColorReset)
}

func Good(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	fmt.Printf("[%s] %s%s%s\n", timestamp(), ColorGreen, formatted, ColorReset)
}

func Warn(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	fmt.Printf("[%s] %s%s%s\n", timestamp(), ColorYellow, formatted, ColorReset)
}

func Error(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	fmt.Printf("[%s] %s%s%s\n", timestamp(), ColorRed, formatted, ColorReset)
}

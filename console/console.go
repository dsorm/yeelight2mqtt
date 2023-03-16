package console

import (
	"fmt"
	"time"
)

func Logf(format string, a ...interface{}) {
	format = time.Now().Format("[2006-01-02 15:04:05] ") + format
	fmt.Printf(format, a...)
}

func Logln(a ...interface{}) {
	timeStr := time.Now().Format("[2006-01-02 15:04:05]")
	a = append([]interface{}{timeStr}, a...)
	fmt.Println(a...)
}

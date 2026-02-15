// Copyright (c) 2021 Shivaram Lingamneni
// released under the 0BSD license

package lib

import (
	"log"
	"runtime/debug"
	"time"
)

// HandlePanic is a generic panic handler that can restart a long-running
// task if necessary. Call it like:
// defer HandlePanic(nil)
// defer HandlePanic(this.method)
func HandlePanic(restartable func()) {
	if r := recover(); r != nil {
		log.Printf("Panic encountered: %v\n%s", r, debug.Stack())
		if restartable != nil {
			time.Sleep(time.Second)
			go restartable()
		}
	}
}

// Copyright (c) 2022 Shivaram Lingamneni
// released under the 0BSD license

package lib

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

/*
   AutoreloadingConfigStore is a configuration file store with the following properties:
   1. If Initialize() succeeds, subsequent calls to Get() return a valid (possibly stale) value
   2. Get() is a wait-free atomic pointer load
   3. The configuration is atomically reloaded in the background if stat(2) shows an updated mtime
   4. ReloadIfChanged() synchronously checks the mtime and returns an up-to-date value, reloading if necessary

Example usage:

	// app-level global:
	var cfg AutoreloadingConfigStore[Config]

	// in main() or similar:
	cfg = AutoreloadingConfigStore[Config]{
		Path: "./config.json",
		LoadCallback: loadConfig,
		CheckInterval: 10*time.Second,
	}
	if _, err := cfg.Initialize(); err != nil {
		log.Fatal(err)
	}
	// cfg.Get() is now safe for any goroutine to call
	go runApp()
*/

type AutoreloadingConfigStore[T any] struct {
	// Path is the path to the config file being monitored.
	Path string
	// LoadCallback is a function that takes a filesystem path and loads the
	// config file at that path, performing any necessary postprocessing and
	// validation. It can return a non-nil error to indicate an invalid file;
	// in this case, the stored value will not be updated (except during
	// Initialize(), when there is no existing stored value to prefer).
	LoadCallback func(string) (*T, error)
	// CheckInterval is the interval on which we check for updates to the file.
	// A zero value means automatic scheduled checks are disabled.
	CheckInterval time.Duration

	stateMutex  sync.Mutex
	value       atomic.Pointer[T]
	mtime       time.Time
	reloadTimer *time.Timer
	stopped     bool
}

// Initialize initializes the store, performing an initial load and returning
// the value, with the load error if applicable. If autoreloading is enabled,
// attempts to autoreload are scheduled even if the initial load returned
// an error.
func (a *AutoreloadingConfigStore[T]) Initialize() (value *T, err error) {
	mtime := getMtime(a.Path)
	value, err = a.LoadCallback(a.Path)

	a.stateMutex.Lock()
	defer a.stateMutex.Unlock()
	a.value.Store(value)
	a.mtime = mtime
	if a.CheckInterval != 0 {
		a.reloadTimer = time.AfterFunc(a.CheckInterval, a.autoreload)
	}
	return
}

// Get returns the most recent valid value of the config. It is wait-free.
func (a *AutoreloadingConfigStore[T]) Get() *T {
	return a.value.Load()
}

// ReloadIfChanged synchronously checks if the config has been updated on
// disk. If it has not been updated, it returns the existing stored value.
// If it has been updated, it reloads the config. If the config loads without
// an error, it updates the stored value and returns it. If it loads with
// an error, it returns the previously stored value, but with the error
// value from loading the new config.
func (a *AutoreloadingConfigStore[T]) ReloadIfChanged() (*T, error) {
	a.stateMutex.Lock()
	mtime := a.mtime
	value := a.value.Load()
	a.stateMutex.Unlock()

	if curMtime := getMtime(a.Path); curMtime.After(mtime) {
		return a.Reload()
	} else {
		return value, nil
	}
}

// Reload synchronously and unconditionally reloads the config. If the config
// loads without an error, it updates the stored value and returns it. If it
// loads with an error, it returns the previously stored value, but with the
// error value from loading the new config.
func (a *AutoreloadingConfigStore[T]) Reload() (*T, error) {
	mtime := getMtime(a.Path)
	value, err := a.LoadCallback(a.Path)

	if err != nil {
		// return the stale value with the error
		return a.Get(), err
	}

	a.stateMutex.Lock()
	defer a.stateMutex.Unlock()
	a.value.Store(value)
	a.mtime = mtime
	return value, nil
}

// Stop prevents the config from autoreloading further (enabling the
// AutoreloadingConfigStore to be garbage-collected).
func (a *AutoreloadingConfigStore[T]) Stop() {
	a.stateMutex.Lock()
	defer a.stateMutex.Unlock()
	a.stopped = true
	if a.reloadTimer != nil {
		// the current timer might have already fired;
		// in that case, the reschedule operation will see `stopped`
		// and refuse to reschedule
		a.reloadTimer.Stop()
	}
}

func (a *AutoreloadingConfigStore[T]) autoreload() {
	// reschedule ourself:
	defer func() {
		a.stateMutex.Lock()
		defer a.stateMutex.Unlock()
		// defensively check that the client didn't set CheckInterval to zero:
		if !a.stopped && a.CheckInterval != 0 {
			a.reloadTimer.Stop()
			a.reloadTimer.Reset(a.CheckInterval)
		}
	}()

	a.ReloadIfChanged()
}

// return the mtime; if the file is inaccessible, return the zero value of time.Time
func getMtime(path string) (mtime time.Time) {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return
}

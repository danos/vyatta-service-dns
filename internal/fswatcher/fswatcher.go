// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: MPL-2.0
package fswatcher

import (
	"log"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	done          chan struct{}
	logPrefix     string
	logger        *log.Logger
	nameToHandler map[string]interface{}
}

type WatcherOpt func(w *Watcher)

func LogPrefix(pfx string) WatcherOpt {
	return func(w *Watcher) {
		w.logPrefix = pfx
	}
}

func Logger(logger *log.Logger) WatcherOpt {
	return func(w *Watcher) {
		w.logger = logger
	}
}

func Handler(filename string, handler interface{}) WatcherOpt {
	return func(w *Watcher) {
		w.nameToHandler[filename] = handler
	}
}

func Start(opts ...WatcherOpt) *Watcher {
	watcher := &Watcher{
		nameToHandler: make(map[string]interface{}),
		done:          make(chan struct{}),
		logger:        log.New(os.Stdout, "", 0),
	}
	for _, opt := range opts {
		opt(watcher)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go watcher.run(&wg)
	wg.Wait()
	return watcher
}

func (w *Watcher) run(wg *sync.WaitGroup) {
	logPrefix := w.logPrefix
	done := w.done

	wg.Done()

	if len(w.nameToHandler) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	for file := range w.nameToHandler {
		watcher.Add(file)
	}

	for {
		select {
		case event := <-watcher.Events:
			handler, ok := w.nameToHandler[event.Name]
			if !ok || handler == nil {
				continue
			}
			switch {
			case event.Op&fsnotify.Write == fsnotify.Write:
				h, ok := handler.(interface {
					Write(string) error
				})
				if !ok {
					continue
				}
				err := h.Write(event.Name)
				if err != nil {
					w.logger.Println(logPrefix, err)
				}
			case event.Op&fsnotify.Create == fsnotify.Create:
				h, ok := handler.(interface {
					Create(string) error
				})
				if !ok {
					continue
				}
				err := h.Create(event.Name)
				if err != nil {
					w.logger.Println(logPrefix, err)
				}
			case event.Op&fsnotify.Rename == fsnotify.Rename:
				h, ok := handler.(interface {
					Rename(string) error
				})
				if !ok {
					continue
				}
				err := h.Rename(event.Name)
				if err != nil {
					w.logger.Println(logPrefix, err)
				}
			case event.Op&fsnotify.Remove == fsnotify.Remove:
				h, ok := handler.(interface {
					Remove(string) error
				})
				if !ok {
					continue
				}
				err := h.Remove(event.Name)
				if err != nil {
					w.logger.Println(logPrefix, err)
				}
			case event.Op&fsnotify.Chmod == fsnotify.Chmod:
				h, ok := handler.(interface {
					Chmod(string) error
				})
				if !ok {
					continue
				}
				err := h.Chmod(event.Name)
				if err != nil {
					w.logger.Println(logPrefix, err)
				}
			}
		case err := <-watcher.Errors:
			w.logger.Println(logPrefix, err)
		case <-done:
			w.logger.Println(logPrefix, "stopping")
			return
		}
	}
}

func (w *Watcher) Stop() {
	close(w.done)
}

// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: MPL-2.0
package fswatcher

import (
	"log"
	"os"
	"path/filepath"
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

func (w *Watcher) getHandler(name string) (interface{}, bool) {
	handler, ok := w.nameToHandler[name]
	return handler, ok
}

func (w *Watcher) dispatchEvent(event fsnotify.Event, handler interface{}) {
	logPrefix := w.logPrefix
	switch {
	case event.Op&fsnotify.Write == fsnotify.Write:
		h, ok := handler.(writeHandler)
		if !ok {
			return
		}
		err := h.Write(event.Name)
		if err != nil {
			w.logger.Println(logPrefix, err)
		}
	case event.Op&fsnotify.Create == fsnotify.Create:
		h, ok := handler.(createHandler)
		if !ok {
			return
		}
		err := h.Create(event.Name)
		if err != nil {
			w.logger.Println(logPrefix, err)
		}
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		h, ok := handler.(renameHandler)
		if !ok {
			return
		}
		err := h.Rename(event.Name)
		if err != nil {
			w.logger.Println(logPrefix, err)
		}
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		h, ok := handler.(removeHandler)
		if !ok {
			return
		}
		err := h.Remove(event.Name)
		if err != nil {
			w.logger.Println(logPrefix, err)
		}
	case event.Op&fsnotify.CloseWrite == fsnotify.CloseWrite:
		h, ok := handler.(closeWriteHandler)
		if !ok {
			return
		}
		err := h.CloseWrite(event.Name)
		if err != nil {
			w.logger.Println(logPrefix, err)
		}
	}
}

func (w *Watcher) run(wg *sync.WaitGroup) {
	logPrefix := w.logPrefix
	done := w.done

	if len(w.nameToHandler) == 0 {
		wg.Done()
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	for file := range w.nameToHandler {
		watcher.Add(filepath.Dir(file))
	}

	wg.Done()

	for {
		select {
		case event := <-watcher.Events:
			handler, ok := w.getHandler(event.Name)
			if !ok || handler == nil {
				continue
			}
			w.dispatchEvent(event, handler)
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

type writeHandler interface {
	Write(string) error
}

type createHandler interface {
	Create(string) error
}

type renameHandler interface {
	Rename(string) error
}

type removeHandler interface {
	Remove(string) error
}

type closeWriteHandler interface {
	CloseWrite(string) error
}

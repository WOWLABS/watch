package watch

import (
	"html/template"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
)

type HTML struct {
	files     map[string]htmlTmplFuncPair // { template, func map } set is mapped to path string
	lock      sync.RWMutex                // make map thread safe
	watcher   *fsnotify.Watcher           // to watch for changes in the files
	done      chan struct{}               // to signal done and cleanup
	closeOnce sync.Once                   // only allow sending on done channel once
}

type htmlTmplFuncPair struct {
	Template *template.Template
	FuncMap  template.FuncMap
}

// HTMLFiles builds a new HTML file template watcher.
func HTMLFiles() (*HTML, error) {
	hfile := new(HTML)

	var err error
	hfile.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	hfile.files = make(map[string]htmlTmplFuncPair)
	hfile.done = make(chan struct{}, 1)

	return hfile, nil
}

// AddFiles adds the files to be watched. It returns an error if a file does not exist,
// or if there was a problem parsing the template.
func (hfile *HTML) AddFiles(paths ...string) error {
	cntxLog := log.WithFields(log.Fields{
		"func":  "watch.HTML.AddFiles",
		"paths": paths,
	})

	for i := range paths {
		cntxLog = cntxLog.WithField("path", paths[i])
		cntxLog.Debug("parsing template")
		tmpl, err := template.New(paths[i]).ParseFiles(paths[i])
		if err != nil {
			return err
		}

		cntxLog.Debug("adding file to watcher")
		if err := hfile.watcher.Add(paths[i]); err != nil {
			return err
		}

		cntxLog.Debug("adding html tmpl pair to inner map")
		hfile.lock.Lock()
		hfile.files[paths[i]] = htmlTmplFuncPair{Template: tmpl}
		hfile.lock.Unlock()
	}

	return nil
}

// AddFileWithFunc adds the file to be watched, with its corresponding function map. It
// returns an error if a file does not exist, or if there was a problem parsing the template.
func (hfile *HTML) AddFileWithFunc(path string, funcMap template.FuncMap) error {
	tmpl, err := template.New(path).Funcs(funcMap).ParseFiles(path)
	if err != nil {
		return err
	}

	if err := hfile.watcher.Add(path); err != nil {
		return err
	}

	hfile.lock.Lock()
	hfile.files[path] = htmlTmplFuncPair{Template: tmpl, FuncMap: funcMap}
	hfile.lock.Unlock()

	return nil
}

// File gets a template file that is being watched. If a path is given for a file
// that is not being watched, a nil pointer is returned, with an os.ErrNotExist error.
func (hfile *HTML) File(path string) (*template.Template, error) {
	hfile.lock.RLock()
	t, ok := hfile.files[path]
	hfile.lock.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}

	return t.Template, nil
}

// Watch begins watching the HTML template files added by AddFiles. When a write event occurs on the watched
// file, its template is rebuilt. If an error is encountered while rebuilding the template, the error is logged,
// and the new template is discarded.
func (hfile *HTML) Watch() {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.HTML.Watch",
	})

	cntxLog.Warn("beginning watch")
	log.SetLevel(log.DebugLevel)

	for {
		select {
		case event := <-hfile.watcher.Events:
			cntxLog := cntxLog.WithFields(log.Fields{
				"eventName": event.Name,
				"eventOp":   event.Op,
			})

			cntxLog.Debug("got event")

			/*
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					for i := 0; i < 3; i++ {
						if err := hfile.watcher.Add(event.Name); err != nil {
							cntxLog.WithError(err).Error("error adding after remove event")
							time.Sleep(250 * time.Millisecond)
						} else {
							break
						}
					}
					continue
				}*/

			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Remove == fsnotify.Remove {

				hfile.lock.RLock()
				pair, ok := hfile.files[event.Name]
				hfile.lock.RUnlock()

				if !ok {
					// should never happen
					cntxLog.Panic("notification on file not being watched")
				}

				cntxLog.Debug("recompiling template on write")

				var tmpl *template.Template
				var err error
				if pair.FuncMap != nil {
					tmpl, err = template.New(event.Name).Funcs(pair.FuncMap).ParseFiles(event.Name)
				} else {
					tmpl, err = template.New(event.Name).ParseFiles(event.Name)
				}

				if err != nil {
					cntxLog.WithError(err).Warn("error compiling template")
					continue
				}

				hfile.lock.Lock()
				hfile.files[event.Name] = htmlTmplFuncPair{Template: tmpl, FuncMap: pair.FuncMap}
				hfile.lock.Unlock()

				cntxLog.Info("template compiled")

				if err := hfile.watcher.Add(event.Name); err != nil {
					cntxLog.WithError(err).Error("error adding after remove event")
				}

			}
		case <-hfile.done:
			if err := hfile.watcher.Close(); err != nil {
				cntxLog.WithError(err).Error("error closing watcher")
			} else {
				cntxLog.Info("done watching")
			}

			close(hfile.done)
			return
		case err := <-hfile.watcher.Errors:
			cntxLog.Error(err)
		}
	}
}

// Stop stops watching for changes in files, and cleans up resources.
func (hfile *HTML) Stop() {
	hfile.closeOnce.Do(func() {
		hfile.done <- struct{}{}
	})
}

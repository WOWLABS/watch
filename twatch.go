package watch

import (
	"os"
	"sync"
	"text/template"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
)

type Text struct {
	files     map[string]txtTmplFuncPair // { template, func map } set is mapped to path string
	lock      sync.RWMutex               // make map thread safe
	watcher   *fsnotify.Watcher          // to watch for changes in the files
	done      chan struct{}              // to signal done and cleanup
	closeOnce sync.Once                  // only allow sending on done channel once
}

type txtTmplFuncPair struct {
	Template *template.Template
	FuncMap  template.FuncMap
}

// TextFiles builds a new text file template watcher.
func TextFiles() (*Text, error) {
	tfile := new(Text)

	var err error
	tfile.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	tfile.files = make(map[string]txtTmplFuncPair)
	tfile.done = make(chan struct{}, 1)

	return tfile, nil
}

// AddFiles adds the files to be watched. It returns an error if a file does not exist,
// or if there was a problem parsing the template.
func (tfile *Text) AddFiles(paths ...string) error {
	for i := range paths {
		tmpl, err := template.New(paths[i]).ParseFiles(paths[i])
		if err != nil {
			return err
		}

		tfile.lock.Lock()
		tfile.files[paths[i]] = txtTmplFuncPair{Template: tmpl}
		tfile.lock.Unlock()
	}

	return nil
}

// AddFileWithFunc adds the file to be watched, with its corresponding function map. It
// returns an error if a file does not exist, or if there was a problem parsing the template.
func (tfile *Text) AddFileWithFunc(path string, funcMap template.FuncMap) error {
	tmpl, err := template.New(path).Funcs(funcMap).ParseFiles(path)
	if err != nil {
		return err
	}

	tfile.lock.Lock()
	tfile.files[path] = txtTmplFuncPair{Template: tmpl, FuncMap: funcMap}
	tfile.lock.Unlock()

	return nil
}

// File gets a template file that is being watched. If a path is requested for a file
// that is not being watched, a nil pointer is returned, with an os.ErrNotExist error.
func (tfile *Text) File(path string) (*template.Template, error) {
	tfile.lock.RLock()
	t, ok := tfile.files[path]
	tfile.lock.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}

	return t.Template, nil
}

// Watch begins watching the text template files added by AddFiles. When a write event occurs on the watched
// file, its template is rebuilt. If an error is encountered while rebuilding the template, the error is logged,
// and the new template is discarded.
func (tfile *Text) Watch() {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.Text.Watch",
	})

	cntxLog.Debug("beginning watch")
	for {
		select {
		case event := <-tfile.watcher.Events:
			cntxLog := cntxLog.WithFields(log.Fields{
				"event": event.Name,
				"op":    event.Op,
			})
			cntxLog.Debug("got event")
			if event.Op&fsnotify.Write == fsnotify.Write {

				tfile.lock.RLock()
				pair, ok := tfile.files[event.Name]
				tfile.lock.RUnlock()

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

				tfile.lock.Lock()
				tfile.files[event.Name] = txtTmplFuncPair{Template: tmpl, FuncMap: pair.FuncMap}
				tfile.lock.Unlock()

				cntxLog.Info("template compiled")
			}
		case <-tfile.done:
			if err := tfile.watcher.Close(); err != nil {
				cntxLog.WithError(err).Error("error closing watcher")
			} else {
				cntxLog.Info("done watching")
			}

			close(tfile.done)
			return
		case err := <-tfile.watcher.Errors:
			cntxLog.Error(err)
		}
	}
}

// Stop stops watching for changes in files, and cleans up resources.
func (tfile *Text) Stop() {
	tfile.closeOnce.Do(func() { // protect against sending over closed channel
		tfile.done <- struct{}{}
	})
}

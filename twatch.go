package watch

import (
	"os"
	"sync"
	"text/template"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
)

type textWatcher struct {
	files     map[string]*template.Template // templates mapped to string representing path to file
	lock      sync.RWMutex                  // make map thread safe
	watcher   *fsnotify.Watcher             // to watch for changes in the files
	done      chan struct{}                 // to signal done and cleanup
	closeOnce sync.Once                     // only allow sending on done channel once
}

// TextFiles builds a new text file template watcher.
func TextFiles() (*textWatcher, error) {
	tfile := new(textWatcher)

	var err error
	tfile.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	tfile.files = make(map[string]*template.Template)
	tfile.done = make(chan struct{}, 1)

	return tfile, nil
}

// AddFiles adds the files to be watched. It returns an error if a file does not exist,
// or if there was a problem parsing the template.
func (tfile *textWatcher) AddFiles(paths ...string) error {
	for i := range paths {
		tmpl, err := template.New(paths[i]).ParseFiles(paths[i])
		if err != nil {
			return err
		}

		tfile.lock.Lock()
		tfile.files[paths[i]] = tmpl
		tfile.lock.Unlock()
	}

	return nil
}

// File gets a template file that is being watched. If a path is given for a file
// that is not being watched, a nil pointer is returned, with an os.ErrNotExist error.
func (tfile *textWatcher) File(path string) (*template.Template, error) {
	tfile.lock.RLock()
	t, ok := tfile.files[path]
	tfile.lock.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}

	return t, nil
}

// Watch begins watching the text template files added by AddFiles. When a write event occurs on the watched
// file, its template is rebuilt. If an error is encountered while rebuilding the template, the error is logged,
// and the new template is discarded.
func (tfile *textWatcher) Watch() {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.Text.Watch",
	})

	for {
		select {
		case event := <-tfile.watcher.Events:
			cntxLog := cntxLog.WithFields(log.Fields{
				"event": event.Name,
				"op":    event.Op,
			})
			cntxLog.Debug("got event")
			if event.Op&fsnotify.Write == fsnotify.Write {
				cntxLog.Debug("recompiling template on write")
				tmpl, err := template.New(event.Name).ParseFiles(event.Name)
				if err != nil {
					cntxLog.WithError(err).Warn("error compiling template")
					continue
				}

				tfile.lock.Lock()
				tfile.files[event.Name] = tmpl
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
func (tfile *textWatcher) Stop() {
	tfile.closeOnce.Do(func() {
		tfile.done <- struct{}{}
	})
}

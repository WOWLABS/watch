package watch

import (
	"html/template"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
)

type htmlWatcher struct {
	files     map[string]*template.Template // templates mapped to string representing path to file
	lock      sync.RWMutex                  // make map thread safe
	watcher   *fsnotify.Watcher             // to watch for changes in the files
	done      chan struct{}                 // to signal done and cleanup
	closeOnce sync.Once                     // only allow sending on done channel once
}

// HTMLFiles builds a new HTML file template watcher.
func HTMLFiles() (*htmlWatcher, error) {
	hfile := new(htmlWatcher)

	var err error
	hfile.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	hfile.files = make(map[string]*template.Template)
	hfile.done = make(chan struct{}, 1)

	return hfile, nil
}

// AddFiles adds the files to be watched. It returns an error if a file does not exist,
// or if there was a problem parsing the template.
func (hfile *htmlWatcher) AddFiles(paths ...string) error {
	for i := range paths {
		tmpl, err := template.New(paths[i]).ParseFiles(paths[i])
		if err != nil {
			return err
		}

		hfile.lock.Lock()
		hfile.files[paths[i]] = tmpl
		hfile.lock.Unlock()
	}

	return nil
}

// File gets a template file that is being watched. If a path is given for a file
// that is not being watched, a nil pointer is returned, with an os.ErrNotExist error.
func (hfile *htmlWatcher) File(path string) (*template.Template, error) {
	hfile.lock.RLock()
	t, ok := hfile.files[path]
	hfile.lock.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}

	return t, nil
}

// Watch begins watching the HTML template files added by AddFiles. When a write event occurs on the watched
// file, its template is rebuilt. If an error is encountered while rebuilding the template, the error is logged,
// and the new template is discarded.
func (hfile *htmlWatcher) Watch() {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.HTML.Watch",
	})

	for {
		select {
		case event := <-hfile.watcher.Events:
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

				hfile.lock.Lock()
				hfile.files[event.Name] = tmpl
				hfile.lock.Unlock()

				cntxLog.Info("template compiled")
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
func (hfile *htmlWatcher) Stop() {
	hfile.closeOnce.Do(func() {
		hfile.done <- struct{}{}
	})
}

// Package watch implements watching of templates on the filesystem. When a write event occurs,
// the watched template is reread, rebuilt, and is ready for use.
package watch

import (
	"html/template"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
	"path/filepath"
)

// HTML and watches HTML template files for write events. If a write is received,
// the template is reread from the filesystem, and recompiles. If no error occurs,
// the old template is replaced with the new one.
type HTML struct {
	files     map[string]htmlTmplFuncPair // { template, func map } pair is mapped to path string
	lock      sync.RWMutex                // files map lock
	watcher   *fsnotify.Watcher           // watches for changes in the files
	done      chan struct{}               // signals done and cleanup
	closeOnce sync.Once                   // only allow sending on done channel once
}

// htmlTmplFuncPair keeps the HTML template and its FuncMap together.
type htmlTmplFuncPair struct {
	Template *template.Template
	FuncMap  template.FuncMap
}

// HTMLFiles builds and initializes a new HTML file template watcher.
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

// AddFiles adds the HTML template files to be watched. It returns an error if
// a file does not exist, or if there was a problem parsing the template. If an
// error occurs, the template is not added.
func (hfile *HTML) AddFiles(paths ...string) error {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.HTML.AddFiles",
	})

	for i := range paths {
		cntxLog = cntxLog.WithField("path", paths[i])
		cntxLog.Debugf("parsing template %d", i)
		tmpl, err := template.New(filepath.Base(paths[i])).ParseFiles(paths[i])
		if err != nil {
			cntxLog.WithError(err).Error("error parsing template")
			return err
		}

		cntxLog.Debug("adding file to watcher")
		if err := hfile.watcher.Add(paths[i]); err != nil {
			cntxLog.WithError(err).Error("error adding file to watcher")
			return err
		}

		cntxLog.Debug("adding html tmpl pair to map")
		hfile.lock.Lock()
		hfile.files[paths[i]] = htmlTmplFuncPair{Template: tmpl}
		hfile.lock.Unlock()
	}

	return nil
}

// AddFileWithFunc adds the template file to be watched, with a function map.
// An error is returned if the file at the path given does not exist,
// or if there was a problem parsing the template.
func (hfile *HTML) AddFileWithFunc(path string, funcMap template.FuncMap) error {
	cntxLog := log.WithFields(log.Fields{
		"func": "watch.HTML.AddFileWithFunc",
		"path": path,
	})

	cntxLog.Debug("parsing template")
	// use filpath.Base to use basename of path for template name, because if template
	// name doesn't match path basename, when using ParseFiles, ExecuteTemplate must
	// explicitly be used when executing the template (Execute will fail)
	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).ParseFiles(path)
	if err != nil {
		cntxLog.Error(err)
		return ParseTmplErr
	}

	cntxLog.Debug("adding file to watcher")
	if err := hfile.watcher.Add(path); err != nil {
		cntxLog.Error(err)
		return AddFileErr
	}

	hfile.lock.Lock()
	hfile.files[path] = htmlTmplFuncPair{Template: tmpl, FuncMap: funcMap}
	hfile.lock.Unlock()

	return nil
}

// File returns the parsed template for the file being watched at the given path.
// If a path is given for a file that is not being watched, a nil pointer is returned,
// with an os.ErrNotExist error.
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

	cntxLog.Debug("beginning watch")

	for {
		select {
		case event := <-hfile.watcher.Events:
			cntxLog := cntxLog.WithFields(log.Fields{
				"path": event.Name,
				"op":   event.Op,
			})

			cntxLog.Debug("got event")

			if event.Op&fsnotify.Write == fsnotify.Write {

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
					tmpl, err = template.New(filepath.Base(event.Name)).Funcs(pair.FuncMap).ParseFiles(event.Name)
				} else {
					tmpl, err = template.New(filepath.Base(event.Name)).ParseFiles(event.Name)
				}

				if err != nil {
					cntxLog.WithError(err).Warn("error compiling template")
					continue
				}

				hfile.lock.Lock()
				hfile.files[event.Name] = htmlTmplFuncPair{Template: tmpl, FuncMap: pair.FuncMap}
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
func (hfile *HTML) Stop() {
	hfile.closeOnce.Do(func() {
		hfile.done <- struct{}{}
	})
}

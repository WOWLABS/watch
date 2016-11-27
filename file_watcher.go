package watch

import "github.com/alecthomas/template"

type FileWatcher interface {
	AddFiles(path ...string) error
	AddFileWithFunc(path string, funcMap template.FuncMap)
	File(path string) (*template.Template, error)
	Watch()
	Stop()
}

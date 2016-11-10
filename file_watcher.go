package watch

import "github.com/alecthomas/template"

type FileWatcher interface {
	AddFiles(path ...string) error
	File(path string) (*template.Template, error)
	Watch()
	Stop()
}

package watch

import "errors"

var ParseTmplErr error = errors.New("error parsing template")
var AddFileErr error = errors.New("error adding file to watcher")

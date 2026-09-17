package attestpublication

import (
	"errors"
	"os"

	"github.com/sageox/ox/internal/fileutil"
)

// lockPublicationFile takes a non-blocking exclusive lock on the already-open
// publication lock file. Contention is reported as (false, nil); the caller
// treats it as "another publisher is active", not as a failure.
func lockPublicationFile(file *os.File) (bool, error) {
	err := fileutil.TryLock(file)
	if errors.Is(err, fileutil.ErrLocked) {
		return false, nil
	}
	return err == nil, err
}

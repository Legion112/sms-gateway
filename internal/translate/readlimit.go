package translate

import (
	"fmt"
	"io"
)

// readLimited reads at most limit bytes and fails if there is more, rather than
// silently truncating: a response that does not fit is not a translation, and
// pretending the first 64kB of it is one would be worse than saying so.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("translate: response larger than %d bytes", limit)
	}
	return raw, nil
}

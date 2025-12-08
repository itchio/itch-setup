package data

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Embedded installer assets and locale files.
//
//go:embed *.png *.ico locales/*.json
var assets embed.FS

// Asset returns the contents of an embedded file. Paths may be passed with or
// without the leading "data/" prefix.
func Asset(path string) ([]byte, error) {
	normalized := strings.TrimPrefix(path, "data/")

	contents, err := assets.ReadFile(normalized)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: not found", path)
		}

		return nil, err
	}

	return contents, nil
}

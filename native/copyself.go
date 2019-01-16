package native

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pkg/errors"
)

func CopySelf(targetExecPath string) (string, error) {
	log.Printf("Copying self to (%s)", targetExecPath)

	execPath, err := os.Executable()
	if err != nil {
		return "", errors.WithMessage(err, "while getting self path")
	}

	execPath = filepath.Clean(execPath)
	targetExecPath = filepath.Clean(targetExecPath)

	if execPath == targetExecPath {
		log.Printf("Wait, no, (%s) is precisely what we're running off of, skipping...", execPath)
		return targetExecPath, nil
	}

	src, err := os.Open(execPath)
	if err != nil {
		return "", errors.WithMessage(err, "while opening self")
	}

	dst, err := os.Create(targetExecPath)
	if err != nil {
		return "", errors.WithMessage(err, "while creating copy of self in install folder")
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return "", errors.WithMessage(err, "while copying self to install folder")
	}

	if runtime.GOOS != "windows" {
		err = dst.Chmod(0755)
		if err != nil {
			return "", errors.WithMessage(err, "while making copy of self executable")
		}
	}

	return targetExecPath, nil
}

package nwin

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

func CopySelf(installDir string) (string, error) {
	log.Printf("Copying self to (%s)", installDir)

	execPath, err := os.Executable()
	if err != nil {
		return "", errors.WithMessage(err, "while getting self path")
	}

	targetExecPath := filepath.Join(installDir, "itch-setup.exe")

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

	return targetExecPath, nil
}

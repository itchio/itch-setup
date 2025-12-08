package native

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

func CopySelf(targetExecPath string) (string, error) {
	log.Printf("Copying self to (%s)", targetExecPath)

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("while getting self path: %w", err)
	}

	execPath = filepath.Clean(execPath)
	targetExecPath = filepath.Clean(targetExecPath)

	if execPath == targetExecPath {
		log.Printf("Wait, no, (%s) is precisely what we're running off of, skipping...", execPath)
		return targetExecPath, nil
	}

	src, err := os.Open(execPath)
	if err != nil {
		return "", fmt.Errorf("while opening self: %w", err)
	}

	dst, err := os.Create(targetExecPath)
	if err != nil {
		return "", fmt.Errorf("while creating copy of self in install folder: %w", err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return "", fmt.Errorf("while copying self to install folder: %w", err)
	}

	if runtime.GOOS != "windows" {
		err = dst.Chmod(0755)
		if err != nil {
			return "", fmt.Errorf("while making copy of self executable: %w", err)
		}
	}

	return targetExecPath, nil
}

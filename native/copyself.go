package native

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// CopySelf installs the running binary at targetExecPath. The target may
// be executing right now (a launch shim boot, a --run-game session), and
// the update must never leave a truncated binary behind, so the copy is
// written to a sibling temp file and renamed into place: running
// processes keep their old image, and interruptions leave the target
// untouched.
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

	if sameContents(execPath, targetExecPath) {
		log.Printf("(%s) is already identical, skipping", targetExecPath)
		return targetExecPath, nil
	}

	tmpPath := targetExecPath + ".tmp"
	err = writeCopy(execPath, tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	err = os.Rename(tmpPath, targetExecPath)
	if err != nil && runtime.GOOS == "windows" {
		err = renameOverRunningExe(tmpPath, targetExecPath)
	}
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("while installing copy of self: %w", err)
	}

	return targetExecPath, nil
}

func writeCopy(srcPath string, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("while opening self: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("while creating copy of self: %w", err)
	}

	_, err = io.Copy(dst, src)
	if err != nil {
		dst.Close()
		return fmt.Errorf("while copying self: %w", err)
	}

	if runtime.GOOS != "windows" {
		err = dst.Chmod(0755)
		if err != nil {
			dst.Close()
			return fmt.Errorf("while making copy of self executable: %w", err)
		}
	}

	// close errors are write errors on some filesystems
	err = dst.Close()
	if err != nil {
		return fmt.Errorf("while flushing copy of self: %w", err)
	}
	return nil
}

// renameOverRunningExe replaces a Windows exe that may be executing:
// rename can't delete a running exe, but renaming the running exe aside
// is allowed. The .old file is removed best-effort; while the old binary
// still runs it stays behind and the next swap cleans it up.
func renameOverRunningExe(tmpPath string, targetExecPath string) error {
	oldPath := targetExecPath + ".old"
	os.Remove(oldPath)

	err := os.Rename(targetExecPath, oldPath)
	if err != nil {
		return err
	}
	err = os.Rename(tmpPath, targetExecPath)
	if err != nil {
		// put the previous binary back rather than leaving nothing
		if restoreErr := os.Rename(oldPath, targetExecPath); restoreErr != nil {
			log.Printf("Could not restore previous binary: %+v", restoreErr)
		}
		return err
	}
	os.Remove(oldPath)
	return nil
}

func sameContents(aPath string, bPath string) bool {
	aStat, err := os.Stat(aPath)
	if err != nil {
		return false
	}
	bStat, err := os.Stat(bPath)
	if err != nil {
		return false
	}
	if aStat.Size() != bStat.Size() {
		return false
	}
	aSum, err := fileSum(aPath)
	if err != nil {
		return false
	}
	bSum, err := fileSum(bPath)
	if err != nil {
		return false
	}
	return bytes.Equal(aSum, bSum)
}

func fileSum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

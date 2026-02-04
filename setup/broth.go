package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	itchio "github.com/itchio/go-itchio"
)

type BrothBuildInfo struct {
	Version string            `json:"version"`
	Files   []*BrothBuildFile `json:"files"`
}

type BrothBuildFile struct {
	Type    itchio.BuildFileType    `json:"type"`
	SubType itchio.BuildFileSubType `json:"subType"`
	Size    int64                   `json:"size"`
}

type BrothUpgradePath struct {
	Patches []*BrothPatch `json:"patches"`
}

type BrothPatch struct {
	Version string            `json:"version"`
	Files   []*BrothPatchFile `json:"files"`
}

func (bp *BrothPatch) FindSubType(subType itchio.BuildFileSubType) *BrothPatchFile {
	for _, f := range bp.Files {
		if f.SubType == subType {
			return f
		}
	}
	return nil
}

type BrothPatchFile struct {
	SubType itchio.BuildFileSubType `json:"subType"`
	Size    int64                   `json:"size"`
}

func (i *Installer) brothGetBytes(format string, args ...interface{}) ([]byte, error) {
	subpath := fmt.Sprintf(format, args...)
	url := fmt.Sprintf("%s/%s", i.brothPackageURL(), strings.Trim(subpath, "/"))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("Could not build GET request to %s: %w", url, err)
	}

	res, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("While performing GET request to %s: %w", url, err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Got HTTP %d for %s", res.StatusCode, url)
	}

	bs, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("While reading GET request to %s: %w", url, err)
	}

	return bs, nil
}

func (i *Installer) brothGetString(format string, args ...interface{}) (string, error) {
	bs, err := i.brothGetBytes(format, args...)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(bs)), nil
}

func (i *Installer) brothGetResponse(r interface{}, format string, args ...interface{}) error {
	bs, err := i.brothGetBytes(format, args...)
	if err != nil {
		return err
	}

	err = json.Unmarshal(bs, r)
	if err != nil {
		return fmt.Errorf("unmarshalling broth response: %w", err)
	}

	return nil
}

// checkChannelExists returns true if the channel exists, false if 404, or error for other failures
func (i *Installer) checkChannelExists() (bool, error) {
	url := fmt.Sprintf("%s/%s/%s/LATEST", brothBaseURL, i.settings.AppName, i.channelName)
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, err
	}
	res, err := i.client.Do(req)
	if err != nil {
		return false, err
	}
	res.Body.Close()

	if res.StatusCode == 404 {
		return false, nil
	}
	if res.StatusCode != 200 {
		return false, fmt.Errorf("unexpected status %d for %s", res.StatusCode, url)
	}
	return true, nil
}

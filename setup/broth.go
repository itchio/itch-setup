package setup

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	itchio "github.com/itchio/go-itchio"
	"github.com/pkg/errors"
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
		return nil, errors.Errorf("Could not build GET request to %s", url)
	}

	res, err := i.client.Do(req)
	if err != nil {
		return nil, errors.WithMessage(err, fmt.Sprintf("While performing GET request to %s", url))
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, errors.Errorf("Got HTTP %d for %s", res.StatusCode, url)
	}

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.WithMessage(err, fmt.Sprintf("While reading GET request to %s", url))
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
		return errors.WithMessage(err, "unmarshalling broth response")
	}

	return nil
}

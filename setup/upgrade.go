package setup

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/itchio/go-itchio"

	"github.com/itchio/wharf/taskgroup"
	"github.com/pkg/errors"
)

type localState struct {
	appDir  string
	version string
}

type remoteState struct {
	version string
}

type patchPlan struct {
	path      *BrothUpgradePath
	totalSize int64
}

type archivePlan struct {
	totalSize int64
}

func (i *Installer) Upgrade(mv Multiverse) error {
	var ls *localState
	var rs *remoteState

	ctx := context.Background()
	err := taskgroup.Do(ctx,
		// check latest version
		func() error {
			latestVersion, err := i.brothGetString("/LATEST")
			if err != nil {
				return err
			}
			rs = &remoteState{
				version: latestVersion,
			}
			return nil
		},

		// check local version
		func() error {
			appDir, ok := mv.GetValidAppDir()
			if !ok {
				return errors.Errorf("No valid app dir found in %s", mv.GetBaseDir())
			}

			currentVersion := strings.TrimPrefix(filepath.Base(appDir), "app-")

			ls = &localState{
				appDir:  appDir,
				version: currentVersion,
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	log.Printf("Installed %s", ls.version)
	log.Printf("Latest    %s", rs.version)

	if ls.version == rs.version {
		log.Printf("We're up-to-date!")
		return nil
	}

	var pp *patchPlan
	var ap *archivePlan

	err = taskgroup.Do(ctx,
		// try to find patch plan
		func() error {
			upgradePath := &BrothUpgradePath{}
			err = i.brothGetResponse(upgradePath, "/%s/upgrade-paths/%s",
				ls.version,
				rs.version,
			)
			if err != nil {
				log.Printf("While looking for upgrade path: %v", err)
				log.Printf("Giving up patch plan")
				return nil
			}

			vnames := []string{ls.version}
			for _, bp := range upgradePath.Patches {
				vnames = append(vnames, bp.Version)
			}
			log.Printf("Upgrade path: %s",
				strings.Join(vnames, " → "),
			)

			var totalSize int64
			for _, bp := range upgradePath.Patches {
				f := bp.FindSubType(itchio.BuildFileSubTypeDefault)
				if f == nil {
					log.Printf("Missing patch for version %s, giving up patch plan", bp.Version)
					return nil
				}

				of := bp.FindSubType(itchio.BuildFileSubTypeOptimized)
				if of != nil && of.Size < f.Size {
					f = of
				}

				totalSize += f.Size
			}
			pp = &patchPlan{
				path:      upgradePath,
				totalSize: totalSize,
			}

			return nil
		},

		// try to find archive plan
		func() error {
			buildInfo := &BrothBuildInfo{}
			err = i.brothGetResponse(buildInfo, "/%s/info", rs.version)
			if err != nil {
				return errors.WithMessage(err, "While looking for archive plan")
			}

			found := false
			for _, f := range buildInfo.Files {
				if f.Type == itchio.BuildFileTypeArchive &&
					f.SubType == itchio.BuildFileSubTypeDefault {

					ap = &archivePlan{
						totalSize: f.Size,
					}
					found = true
					break
				}
			}

			if !found {
				errMsg := fmt.Sprintf("Default archive not found for version %s", rs.version)
				return errors.WithMessage(err, errMsg)
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	if pp == nil {
		log.Printf("↑ No patch-based upgrade path found")
	} else {
		log.Printf("↑ Patching cost: %s (in %d patches)",
			humanize.IBytes(uint64(pp.totalSize)),
			len(pp.path.Patches),
		)
	}
	log.Printf("↺ Archive  cost: %s",
		humanize.IBytes(uint64(ap.totalSize)),
	)

	return nil
}

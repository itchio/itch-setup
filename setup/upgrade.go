package setup

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/itchio/httpkit/progress"

	"github.com/itchio/wharf/eos/option"

	"github.com/itchio/wharf/pools/fspool"
	"github.com/itchio/wharf/pwr/bowl"

	"github.com/itchio/go-itchio"
	"github.com/itchio/savior/filesource"

	"github.com/itchio/wharf/pwr/patcher"
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
			progress.FormatBytes(pp.totalSize),
			len(pp.path.Patches),
		)
	}
	log.Printf("↺ Archive  cost: %s",
		progress.FormatBytes(ap.totalSize),
	)

	if pp != nil && pp.totalSize < ap.totalSize {
		err = i.applyPatches(mv, ls, pp)
		if err == nil {
			log.Printf("Patching went fine!")
			return nil
		}

		log.Printf("Patching went wrong, falling back to archive.")
		log.Printf("The patching error was: %+v", err)
	}

	return i.applyArchive()
}

func (i *Installer) applyPatches(mv Multiverse, ls *localState, pp *patchPlan) error {
	up := pp.path
	log.Printf("Applying %d patches...", len(up.Patches))

	stagingDir := filepath.Join(mv.GetBaseDir(), ".itch-setup-staging")
	log.Printf("Using (%s) as staging directory", stagingDir)

	err := os.RemoveAll(stagingDir)
	if err != nil {
		return errors.WithMessage(err, "Could not clean staging dir")
	}
	err = os.MkdirAll(stagingDir, 0755)
	if err != nil {
		return errors.WithMessage(err, "Could not make staging dir")
	}
	defer os.RemoveAll(stagingDir)

	applyOne := func(bp *BrothPatch, targetDir string, outputDir string) error {
		log.Printf("Upgrading to %s...", bp.Version)
		f := bp.FindSubType(itchio.BuildFileSubTypeDefault)
		if f == nil {
			return errors.Errorf("Could not find default patch file for version %s, giving up", bp.Version)
		}

		of := bp.FindSubType(itchio.BuildFileSubTypeOptimized)
		if of != nil && of.Size < f.Size {
			f = of
		}
		log.Printf("Using (%s) patch (%s)", f.SubType, progress.FormatBytes(f.Size))

		consumer := newConsumer()

		patchURL := fmt.Sprintf("%s/%s/patch/%s", i.brothPackageURL(), bp.Version, f.SubType)
		log.Printf("☁ %s", patchURL)

		patchSource, err := filesource.Open(patchURL, option.WithConsumer(consumer))
		if err != nil {
			return err
		}
		defer patchSource.Close()

		tracker := progress.NewTracker()
		tracker.SetSilent(true)
		tracker.SetTotalBytes(patchSource.Size())
		tracker.Start()
		defer tracker.Finish()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p, err := patcher.New(patchSource, consumer)
		if err != nil {
			return err
		}

		go func() {
			for {
				select {
				case <-time.After(1 * time.Second):
					tracker.SetProgress(p.Progress())
					log.Printf("%.2f%% done - %s / s, ETA %s",
						tracker.Progress()*100,
						progress.FormatBytes(int64(tracker.BPS())),
						tracker.ETA(),
					)
				case <-ctx.Done():
					return
				}
			}
		}()

		targetPool := fspool.New(p.GetTargetContainer(), targetDir)

		bwl, err := bowl.NewFreshBowl(&bowl.FreshBowlParams{
			TargetContainer: p.GetTargetContainer(),
			TargetPool:      targetPool,
			SourceContainer: p.GetSourceContainer(),
			OutputFolder:    outputDir,
		})
		if err != nil {
			return err
		}

		err = p.Resume(nil, targetPool, bwl)
		if err != nil {
			return err
		}

		err = bwl.Commit()
		if err != nil {
			return err
		}

		return nil
	}

	targetDir := ls.appDir
	var outputDir string
	var latestVersion string
	for _, p := range up.Patches {
		outputDir = filepath.Join(stagingDir, fmt.Sprintf("app-%s", p.Version))
		err := applyOne(p, targetDir, outputDir)
		if err != nil {
			return err
		}
		targetDir = outputDir
		latestVersion = p.Version
	}

	log.Printf("Fully upgraded into (%s)", outputDir)

	finalDir, err := mv.MakeAppDir(latestVersion)
	if err != nil {
		return err
	}

	log.Printf("Moving to (%s)", finalDir)
	err = os.Rename(outputDir, finalDir)
	if err != nil {
		return err
	}
	return nil
}

func (i *Installer) applyArchive() error {
	return errors.Errorf("Apply archive: stub!")
}

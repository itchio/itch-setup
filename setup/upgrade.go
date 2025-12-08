package setup

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/itchio/savior"

	"github.com/itchio/headway/tracker"
	"github.com/itchio/headway/united"

	"github.com/itchio/httpkit/eos"
	"github.com/itchio/httpkit/eos/option"
	"github.com/itchio/httpkit/timeout"

	"github.com/itchio/lake/pools/fspool"

	"github.com/itchio/wharf/pwr"
	"github.com/itchio/wharf/pwr/bowl"
	"github.com/itchio/wharf/pwr/patcher"
	"github.com/itchio/wharf/taskgroup"

	"github.com/itchio/go-itchio"
	"github.com/itchio/savior/filesource"
	"github.com/itchio/savior/zipextractor"
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

type UpgradeResult struct {
	DidUpgrade bool
}

func (i *Installer) Upgrade(mv Multiverse) (*UpgradeResult, error) {
	EnableJSON()
	defer DisableJSON()

	res := &UpgradeResult{}

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
			currentBuild := mv.GetCurrentVersion()
			if currentBuild == nil {
				return fmt.Errorf("No version currently installed")
			}

			ls = &localState{
				appDir:  currentBuild.Path,
				version: currentBuild.Version,
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	log.Printf("Installed %s", ls.version)
	log.Printf("Latest    %s", rs.version)

	if ls.version == rs.version {
		log.Printf("We're up-to-date!")
		Emit(NoUpdateAvailable{})
		return res, nil
	}

	if mv.HasReadyPending() {
		log.Printf("Current is behind, but we have a ready version...")
		if mv.ReadyPendingIs(rs.version) {
			log.Printf("...and it is the latest! (%s)", rs.version)
		}
		Emit(UpdateReady{Version: rs.version})
		res.DidUpgrade = true
		return res, nil
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
				return fmt.Errorf("While looking for archive plan: %w", err)
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
				return fmt.Errorf("Default archive not found for version %s", rs.version)
			}

			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	if pp == nil {
		log.Printf("↑ No patch-based upgrade path found")
	} else {
		log.Printf("↑ Patching cost: %s (in %d patches)",
			united.FormatBytes(pp.totalSize),
			len(pp.path.Patches),
		)
	}
	log.Printf("↺ Archive  cost: %s",
		united.FormatBytes(ap.totalSize),
	)

	if pp != nil && pp.totalSize < ap.totalSize {
		err = i.applyPatches(mv, ls, pp)
		if err == nil {
			log.Printf("Patching went fine!")
			Emit(UpdateReady{Version: rs.version})
			res.DidUpgrade = true
			return res, nil
		}

		log.Printf("Patching went wrong, falling back to archive.")
		log.Printf("The patching error was: %+v", err)
	}

	err = i.applyArchive(mv, rs, ap)
	if err != nil {
		Emit(UpdateFailed{Message: fmt.Sprintf("%+v", err)})
		return nil, err
	}

	Emit(UpdateReady{Version: rs.version})
	res.DidUpgrade = true
	return res, nil
}

func (i *Installer) applyPatches(mv Multiverse, ls *localState, pp *patchPlan) error {
	up := pp.path
	log.Printf("Applying %d patches...", len(up.Patches))

	{
		log.Printf("But first, let's check (%s) is a valid build for (%s)", ls.appDir, ls.version)

		signatureURL := i.buildBrothURL(nil, "%s/signature/default", ls.version)
		log.Printf("☁ %s", signatureURL)

		consumer := newConsumer()
		sigSource, err := filesource.Open(signatureURL, option.WithConsumer(consumer))
		if err != nil {
			return err
		}
		defer sigSource.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sigInfo, err := pwr.ReadSignature(ctx, sigSource)
		if err != nil {
			return err
		}

		vc := pwr.ValidatorContext{
			Consumer: consumer,
			FailFast: true,
		}
		err = vc.Validate(ctx, ls.appDir, sigInfo)
		if err != nil {
			return err
		}
	}

	stagingDir, err := mv.MakeStagingFolder()
	if err != nil {
		return err
	}
	defer mv.CleanStagingFolder()
	log.Printf("Using (%s) as staging directory", stagingDir)

	applyOne := func(bp *BrothPatch, targetDir string, outputDir string) error {
		log.Printf("Upgrading to %s...", bp.Version)
		Emit(InstallingUpdate{Version: bp.Version})

		f := bp.FindSubType(itchio.BuildFileSubTypeDefault)
		if f == nil {
			return fmt.Errorf("Could not find default patch file for version %s, giving up", bp.Version)
		}

		of := bp.FindSubType(itchio.BuildFileSubTypeOptimized)
		if of != nil && of.Size < f.Size {
			f = of
		}
		log.Printf("Using (%s) patch (%s)", f.SubType, united.FormatBytes(f.Size))

		consumer := newConsumer()

		patchURL := i.buildBrothURL(nil, "%s/patch/%s", bp.Version, f.SubType)
		log.Printf("☁ %s", patchURL)

		patchSource, err := filesource.Open(patchURL, option.WithConsumer(consumer))
		if err != nil {
			return err
		}
		defer patchSource.Close()

		tracker := tracker.New(tracker.Opts{
			ByteAmount: &tracker.ByteAmount{
				Value: patchSource.Size(),
			},
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p, err := patcher.New(patchSource, consumer)
		if err != nil {
			return err
		}
		startPrintingProgress(ctx, tracker)

		targetPool := fspool.New(p.GetTargetContainer(), targetDir)

		bwl, err := bowl.NewFreshBowl(bowl.FreshBowlParams{
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
	err = mv.QueueReady(&BuildFolder{
		Version: latestVersion,
		Path:    outputDir,
	})
	if err != nil {
		return err
	}

	return nil
}

func (i *Installer) applyArchive(mv Multiverse, rs *remoteState, ap *archivePlan) error {
	log.Printf("Upgrading to (%s) using archive...", rs.version)
	Emit(InstallingUpdate{Version: rs.version})

	archiveURL := i.buildBrothURL(nil, "%s/archive/default", rs.version)
	log.Printf("☁ %s", archiveURL)

	consumer := newConsumer()

	archiveFile, err := eos.Open(archiveURL, option.WithConsumer(i.consumer))
	if err != nil {
		return err
	}

	archiveStats, err := archiveFile.Stat()
	if err != nil {
		return err
	}

	ex, err := zipextractor.New(archiveFile, archiveStats.Size())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker := tracker.New(tracker.Opts{
		ByteAmount: &tracker.ByteAmount{Value: archiveStats.Size()},
	})

	consumer.OnProgress = tracker.SetProgress
	ex.SetConsumer(consumer)
	startPrintingProgress(ctx, tracker)

	stagingFolder, err := mv.MakeStagingFolder()
	if err != nil {
		return err
	}
	defer mv.CleanStagingFolder()

	outputDir := filepath.Join(stagingFolder, fmt.Sprintf("app-%s", rs.version))
	log.Printf("Extracting %s to (%s)", united.FormatBytes(archiveStats.Size()), outputDir)

	sink := &savior.FolderSink{
		Consumer:  consumer,
		Directory: outputDir,
	}
	var closeSinkOnce sync.Once
	defer closeSinkOnce.Do(func() {
		sink.Close()
	})

	startTime := time.Now()

	res, err := ex.Resume(nil, sink)
	if err != nil {
		return err
	}

	duration := time.Since(startTime)

	log.Printf("Overall extract speed: %s (%s total)",
		united.FormatBPS(res.Size(), duration),
		united.FormatDuration(duration),
	)
	closeSinkOnce.Do(func() {
		sink.Close()
	})

	err = mv.QueueReady(&BuildFolder{
		Version: rs.version,
		Path:    outputDir,
	})
	if err != nil {
		return err
	}

	return nil
}

func startPrintingProgress(ctx context.Context, tracker tracker.Tracker) {
	go func() {
		for {
			select {
			case <-time.After(1 * time.Second):
				p := Progress{
					Progress: tracker.Progress(),
				}
				stats := tracker.Stats()
				if stats != nil {
					if stats.BPS() != nil {
						p.BPS = stats.BPS().Value
					} else {
						p.BPS = timeout.GetBPS()
					}
					if stats.TimeLeft() != nil {
						p.ETA = stats.TimeLeft().Seconds()
					}
				}
				Emit(p)
				log.Printf("%.2f%% done - %s / s, ETA %v",
					tracker.Progress()*100,
					united.FormatBytes(int64(p.BPS)),
					p.ETA,
				)
			case <-ctx.Done():
				return
			}
		}
	}()
}

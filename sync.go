package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Jguer/yay/v11/pkg/db"
	"github.com/Jguer/yay/v11/pkg/dep"
	"github.com/Jguer/yay/v11/pkg/settings"
	"github.com/Jguer/yay/v11/pkg/settings/parser"
	"github.com/Jguer/yay/v11/pkg/text"

	"github.com/leonelquinteros/gotext"
)

func syncInstall(ctx context.Context,
	config *settings.Configuration,
	cmdArgs *parser.Arguments,
	dbExecutor db.Executor,
) error {
	aurCache := config.Runtime.AURCache
	refreshArg := cmdArgs.ExistsArg("y", "refresh")

	if refreshArg && config.Runtime.Mode.AtLeastRepo() {
		if errR := earlyRefresh(ctx, cmdArgs); errR != nil {
			return fmt.Errorf("%s - %w", gotext.Get("error refreshing databases"), errR)
		}

		// we may have done -Sy, our handle now has an old
		// database.
		if errRefresh := dbExecutor.RefreshHandle(); errRefresh != nil {
			return errRefresh
		}
	}

	grapher := dep.NewGrapher(dbExecutor, aurCache, false, settings.NoConfirm, os.Stdout)

	graph, err := grapher.GraphFromTargets(ctx, nil, cmdArgs.Targets)
	if err != nil {
		return err
	}

	if cmdArgs.ExistsArg("u", "sysupgrade") {
		var errSysUp error

		graph, _, errSysUp = sysupgradeTargetsV2(ctx, aurCache, dbExecutor, graph, cmdArgs.ExistsDouble("u", "sysupgrade"))
		if errSysUp != nil {
			return errSysUp
		}
	}

	topoSorted := graph.TopoSortedLayerMap()

	preparer := &Preparer{
		dbExecutor: dbExecutor,
		cmdBuilder: config.Runtime.CmdBuilder,
		config:     config,
	}
	installer := &Installer{dbExecutor: dbExecutor}

	if errP := preparer.Present(os.Stdout, topoSorted); errP != nil {
		return errP
	}

	cleanFunc := preparer.ShouldCleanMakeDeps()
	if cleanFunc != nil {
		installer.AddPostInstallHook(cleanFunc)
	}

	pkgBuildDirs, err := preparer.PrepareWorkspace(ctx, topoSorted)
	if err != nil {
		return err
	}

	err = installer.Install(ctx, cmdArgs, topoSorted, pkgBuildDirs)
	if err != nil {
		if errHook := installer.RunPostInstallHooks(ctx); errHook != nil {
			text.Errorln(errHook)
		}

		return err
	}

	return installer.RunPostInstallHooks(ctx)
}
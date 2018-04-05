/*
 * This file is part of arduino-cli.
 *
 * arduino-cli is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program; if not, write to the Free Software
 * Foundation, Inc., 51 Franklin St, Fifth Floor, Boston, MA  02110-1301  USA
 *
 * As a special exception, you may use this file as part of a free software
 * library without restriction.  Specifically, if other files instantiate
 * templates or use macros or inline functions from this file, or you compile
 * this file and link it with other files to produce an executable, this
 * file does not by itself cause the resulting executable to be covered by
 * the GNU General Public License.  This exception does not however
 * invalidate any other reasons why the executable file might be covered by
 * the GNU General Public License.
 *
 * Copyright 2017-2018 ARDUINO AG (http://www.arduino.cc/)
 */

package packagemanager

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bcmi-labs/arduino-cli/common/releases"
	"github.com/bcmi-labs/arduino-cli/configs"
	"github.com/bcmi-labs/arduino-cli/cores"
	"github.com/bcmi-labs/arduino-cli/cores/packageindex"
	"github.com/sirupsen/logrus"
)

// PackageManager defines the superior oracle which understands all about
// Arduino Packages, how to parse them, download, and so on.
//
// The manager also keeps track of the status of the Packages (their Platform Releases, actually)
// installed in the system.
type PackageManager struct {
	packages *cores.Packages

	// TODO: This might be a list in the future, but would it be of any help?
	eventHandler EventHandler
}

// EventHandler defines the events that are generated by the PackageManager
// Subscribing to such events allows, for instance, to print out logs of what is happening
// (say you use them for a CLI...)
type EventHandler interface {
	// FIXME: This is temporary, for prototyping (an handler should not return an handler; besides, this leakes
	// the usage of releases...)
	OnDownloadingSomething() releases.ParallelDownloadProgressHandler
}

// NewPackageManager returns a new instance of the PackageManager
func NewPackageManager() *PackageManager {
	return &PackageManager{
		packages: cores.NewPackages(),
	}
}

func (pm *PackageManager) Clear() {
	pm.packages = cores.NewPackages()
}

func (pm *PackageManager) EnableDebugOutput() {
	logrus.SetLevel(logrus.DebugLevel)
}

func (pm *PackageManager) DisableDebugOutput() {
	logrus.SetLevel(logrus.ErrorLevel)
}

func (pm *PackageManager) GetPackages() *cores.Packages {
	return pm.packages
}

func (pm *PackageManager) FindBoardsWithVidPid(vid, pid string) []*cores.Board {
	res := []*cores.Board{}
	for _, targetPackage := range pm.packages.Packages {
		for _, targetPlatform := range targetPackage.Platforms {
			if platform := targetPlatform.GetInstalled(); platform != nil {
				for _, board := range platform.Boards {
					if board.HasUsbID(vid, pid) {
						res = append(res, board)
					}
				}
			}
		}
	}
	return res
}

func (pm *PackageManager) FindBoardsWithID(id string) []*cores.Board {
	res := []*cores.Board{}
	for _, targetPackage := range pm.packages.Packages {
		for _, targetPlatform := range targetPackage.Platforms {
			if platform := targetPlatform.GetInstalled(); platform != nil {
				for _, board := range platform.Boards {
					if board.BoardId == id {
						res = append(res, board)
					}
				}
			}
		}
	}
	return res
}

// FindBoardWithFQBN returns the board identified by the fqbn, or an error
func (pm *PackageManager) FindBoardWithFQBN(fqbn string) (*cores.Board, error) {
	// Split fqbn
	fqbnParts := strings.Split(fqbn, ":")
	if len(fqbnParts) < 3 || len(fqbnParts) > 4 {
		return nil, errors.New("incorrect format for fqbn")
	}
	packageName := fqbnParts[0]
	platformArch := fqbnParts[1]
	boardID := fqbnParts[2]

	// Search for the board
	for _, targetPackage := range pm.packages.Packages {
		fmt.Println(targetPackage.Name, packageName)
		if targetPackage.Name != packageName {
			continue
		}
		for _, targetPlatform := range targetPackage.Platforms {
			if targetPlatform.Architecture != platformArch {
				continue
			}

			platform := targetPlatform.GetInstalled()
			if platform == nil {
				return nil, errors.New("platform not installed")
			}
			for _, board := range platform.Boards {
				if board.BoardId == boardID {
					return board, nil
				}
			}
		}
	}
	return nil, errors.New("board not found")
}

// FIXME add an handler to be invoked on each verbose operation, in order to let commands display results through the formatter
// as for the progress bars during download
func (pm *PackageManager) RegisterEventHandler(eventHandler EventHandler) {
	if pm.eventHandler != nil {
		panic("Don't try to register another event handler to the PackageManager yet!")
	}

	pm.eventHandler = eventHandler
}

// GetEventHandlers returns a slice of the registered EventHandlers
func (pm *PackageManager) GetEventHandlers() []*EventHandler {
	return append([]*EventHandler{}, &pm.eventHandler)
}

// LoadPackageIndex loads a package index by looking up the local cached file from the specified URL
func (pm *PackageManager) LoadPackageIndex(URL *url.URL) error {
	indexPath, err := configs.IndexPathFromURL(URL).Get()
	if err != nil {
		return fmt.Errorf("retrieving json index path for %s: %s", URL, err)
	}

	index, err := packageindex.LoadIndex(indexPath)
	if err != nil {
		return fmt.Errorf("loading json index file %s: %s", indexPath, err)
	}

	index.MergeIntoPackages(pm.packages)
	return nil
}

// Package looks for the Package with the given name, returning a structure
// able to perform further operations on that given resource
func (pm *PackageManager) Package(name string) *packageActions {
	//TODO: perhaps these 2 structure should be merged? cores.Packages vs pkgmgr??
	var err error
	thePackage := pm.packages.Packages[name]
	if thePackage == nil {
		err = fmt.Errorf("package '%s' not found", name)
	}
	return &packageActions{
		aPackage:     thePackage,
		forwardError: err,
	}
}

// Actions that can be done on a Package

// packageActions defines what actions can be performed on the specific Package
// It serves as a status container for the fluent APIs
type packageActions struct {
	aPackage     *cores.Package
	forwardError error
}

// Tool looks for the Tool with the given name, returning a structure
// able to perform further operations on that given resource
func (pa *packageActions) Tool(name string) *toolActions {
	var tool *cores.Tool
	err := pa.forwardError
	if err == nil {
		tool = pa.aPackage.Tools[name]

		if tool == nil {
			err = fmt.Errorf("tool '%s' not found in package '%s'", name, pa.aPackage.Name)
		}
	}
	return &toolActions{
		tool:         tool,
		forwardError: err,
	}
}

// END -- Actions that can be done on a Package

// Actions that can be done on a Tool

// toolActions defines what actions can be performed on the specific Tool
// It serves as a status container for the fluent APIs
type toolActions struct {
	tool         *cores.Tool
	forwardError error
}

// Get returns the final representation of the Tool
func (ta *toolActions) Get() (*cores.Tool, error) {
	err := ta.forwardError
	if err == nil {
		return ta.tool, nil
	}
	return nil, err
}

// IsInstalled checks whether any release of the Tool is installed in the system
func (ta *toolActions) IsInstalled() (bool, error) {
	if ta.forwardError != nil {
		return false, ta.forwardError
	}

	for _, release := range ta.tool.Releases {
		if release.IsInstalled() {
			return true, nil
		}
	}
	return false, nil
}

func (ta *toolActions) Release(version string) *toolReleaseActions {
	if ta.forwardError != nil {
		return &toolReleaseActions{forwardError: ta.forwardError}
	}
	release := ta.tool.GetRelease(version)
	if release == nil {
		return &toolReleaseActions{forwardError: fmt.Errorf("release %s not found for tool %s", version, ta.tool.String())}
	}
	return &toolReleaseActions{release: release}
}

// END -- Actions that can be done on a Tool

// toolReleaseActions defines what actions can be performed on the specific ToolRelease
// It serves as a status container for the fluent APIs
type toolReleaseActions struct {
	release      *cores.ToolRelease
	forwardError error
}

func (tr *toolReleaseActions) Get() (*cores.ToolRelease, error) {
	if tr.forwardError != nil {
		return nil, tr.forwardError
	}
	return tr.release, nil
}

func (pm *PackageManager) GetAllInstalledToolsReleases() []*cores.ToolRelease {
	tools := []*cores.ToolRelease{}
	for _, targetPackage := range pm.packages.Packages {
		for _, tool := range targetPackage.Tools {
			for _, release := range tool.Releases {
				if release.IsInstalled() {
					tools = append(tools, release)
				}
			}
		}
	}
	return tools
}

func (pm *PackageManager) FindToolsRequiredForBoard(board *cores.Board) ([]*cores.ToolRelease, error) {
	// core := board.Properties["build.core"]

	platform := board.PlatformRelease

	// maps "PACKAGER:TOOL" => ToolRelease
	foundTools := map[string]*cores.ToolRelease{}

	// a Platform may not specify required tools (because it's a platform that comes from a
	// sketchbook/hardware folder without a package_index.json) then add all available tools
	for _, targetPackage := range pm.packages.Packages {
		for _, tool := range targetPackage.Tools {
			rel := tool.GetLatestInstalled()
			if rel != nil {
				foundTools[rel.Tool.String()] = rel
			}
		}
	}

	// replace the default tools above with the specific required by the current platform
	for _, toolDep := range platform.Dependencies {
		tool := pm.FindToolDependency(toolDep)
		if tool == nil {
			return nil, fmt.Errorf("tool release not found: %s", toolDep)
		}
		foundTools[tool.Tool.String()] = tool
	}

	requiredTools := []*cores.ToolRelease{}
	for _, toolRel := range foundTools {
		requiredTools = append(requiredTools, toolRel)
	}
	return requiredTools, nil
}

func (pm *PackageManager) FindToolDependency(dep *cores.ToolDependency) *cores.ToolRelease {
	toolRelease, err := pm.Package(dep.ToolPackager).Tool(dep.ToolName).Release(dep.ToolVersion).Get()
	if err != nil {
		return nil
	}
	return toolRelease
}

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
 * Copyright 2017 ARDUINO AG (http://www.arduino.cc/)
 */

package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arduino/go-paths-helper"

	builder "github.com/arduino/arduino-builder"
	"github.com/arduino/arduino-builder/types"
	properties "github.com/arduino/go-properties-map"
	"github.com/bcmi-labs/arduino-cli/arduino/cores"
	"github.com/bcmi-labs/arduino-cli/commands"
	"github.com/bcmi-labs/arduino-cli/common/formatter"
	"github.com/bcmi-labs/arduino-cli/common/formatter/output"
	"github.com/bcmi-labs/arduino-cli/configs"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// InitCommand prepares the command.
func InitCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "compile",
		Short:   "Compiles Arduino sketches.",
		Long:    "Compiles Arduino sketches.",
		Example: "arduino compile [sketchPath]",
		Args:    cobra.MaximumNArgs(1),
		Run:     run,
	}
	command.Flags().StringVarP(&flags.fqbn, "fqbn", "b", "", "Fully Qualified Board Name, e.g.: arduino:avr:uno")
	command.Flags().BoolVar(&flags.showProperties, "show-properties", false, "Show all build properties used instead of compiling.")
	command.Flags().BoolVar(&flags.preprocess, "preprocess", false, "Print preprocessed code to stdout instead of compiling.")
	command.Flags().StringVar(&flags.buildCachePath, "build-cache-path", "", "Builds of 'core.a' are saved into this folder to be cached and reused.")
	command.Flags().StringVar(&flags.buildPath, "build-path", "", "Folder where to save compiled files. If omitted, a folder will be created in the temporary folder specified by your OS.")
	command.Flags().StringSliceVar(&flags.buildProperties, "build-properties", []string{}, "List of custom build properties separated by commas. Or can be used multiple times for multiple properties.")
	command.Flags().StringVar(&flags.warnings, "warnings", "none", `Optional, can be "none", "default", "more" and "all". Defaults to "none". Used to tell gcc which warning level to use (-W flag).`)
	command.Flags().BoolVarP(&flags.verbose, "verbose", "v", false, "Optional, turns on verbose mode.")
	command.Flags().BoolVar(&flags.quiet, "quiet", false, "Optional, supresses almost every output.")
	command.Flags().IntVar(&flags.debugLevel, "debug-level", 5, "Optional, defaults to 5. Used for debugging. Set it to 10 when submitting an issue.")
	command.Flags().StringVar(&flags.vidPid, "vid-pid", "", "When specified, VID/PID specific build properties are used, if boards supports them.")
	return command
}

var flags struct {
	fqbn            string   // Fully Qualified Board Name, e.g.: arduino:avr:uno.
	showProperties  bool     // Show all build preferences used instead of compiling.
	preprocess      bool     // Print preprocessed code to stdout.
	buildCachePath  string   // Builds of 'core.a' are saved into this folder to be cached and reused.
	buildPath       string   // Folder where to save compiled files.
	buildProperties []string // List of custom build properties separated by commas. Or can be used multiple times for multiple properties.
	warnings        string   // Used to tell gcc which warning level to use.
	verbose         bool     // Turns on verbose mode.
	quiet           bool     // Supresses almost every output.
	debugLevel      int      // Used for debugging.
	vidPid          string   // VID/PID specific build properties.
}

func run(cmd *cobra.Command, args []string) {
	logrus.Info("Executing `arduino compile`")
	var sketchPath *paths.Path
	if len(args) > 0 {
		sketchPath = paths.New(args[0])
	}
	sketch, err := commands.InitSketch(sketchPath)
	if err != nil {
		formatter.PrintError(err, "Error opening sketch.")
		os.Exit(commands.ErrGeneric)
	}

	fqbn := flags.fqbn
	if fqbn == "" && sketch != nil {
		fqbn = sketch.Metadata.CPU.Fqbn
	}
	if fqbn == "" {
		formatter.PrintErrorMessage("No Fully Qualified Board Name provided.")
		os.Exit(commands.ErrGeneric)
	}
	fqbnParts := strings.Split(fqbn, ":")
	if len(fqbnParts) < 3 || len(fqbnParts) > 4 {
		formatter.PrintErrorMessage("Fully Qualified Board Name has incorrect format.")
		os.Exit(commands.ErrBadArgument)
	}
	packageName := fqbnParts[0]
	coreName := fqbnParts[1]

	pm := commands.InitPackageManager()
	if err := pm.LoadHardware(); err != nil {
		formatter.PrintError(err, "Could not load hardware packages.")
		os.Exit(commands.ErrCoreConfig)
	}

	// Check for ctags tool
	loadBuiltinCtagsMetadata(pm)
	ctags, err := getBuiltinCtagsTool(pm)
	if !ctags.IsInstalled() {
		formatter.Print("Downloading missing tool: " + ctags.String())
		resp, err := pm.DownloadToolRelease(ctags)
		if err != nil {
			formatter.PrintError(err, "Error downloading ctags")
			os.Exit(commands.ErrNetwork)
		}
		formatter.DownloadProgressBar(resp, ctags.String())
		if resp.Err() != nil {
			formatter.PrintError(resp.Err(), "Error downloading ctags")
			os.Exit(commands.ErrNetwork)
		}
		formatter.Print("Installing " + ctags.String())
		res := &output.CoreProcessResults{Tools: map[string]output.ProcessResult{}}
		if err := pm.InstallToolReleases([]*cores.ToolRelease{ctags}, res); err != nil {
			formatter.PrintError(err, "Error installing ctags")
			formatter.PrintErrorMessage("Missing ctags tool.")
			os.Exit(commands.ErrCoreConfig)
		}

		if err := pm.LoadHardware(); err != nil {
			formatter.PrintError(err, "Could not load hardware packages.")
			os.Exit(commands.ErrCoreConfig)
		}
		ctags, err = getBuiltinCtagsTool(pm)
		if !ctags.IsInstalled() {
			formatter.PrintErrorMessage("Missing ctags tool.")
			os.Exit(commands.ErrCoreConfig)
		}
	}

	isCoreInstalled, err := cores.IsCoreInstalled(packageName, coreName)
	if err != nil {
		formatter.PrintError(err, "Cannot check core installation.")
		os.Exit(commands.ErrCoreConfig)
	}
	if !isCoreInstalled {
		formatter.PrintErrorMessage(fmt.Sprintf("\"%[1]s:%[2]s\" core is not installed, please install it by running \"arduino core install %[1]s:%[2]s\".", packageName, coreName))
		os.Exit(commands.ErrCoreConfig)
	}

	ctx := &types.Context{}

	if parsedFqbn, err := cores.ParseFQBN(fqbn); err != nil {
		formatter.PrintError(err, "Error parsing FQBN.")
	} else {
		ctx.FQBN = parsedFqbn
	}
	ctx.SketchLocation = paths.New(sketch.FullPath)

	// FIXME: This will be redundant when arduino-builder will be part of the cli
	if packagesFolder, err := configs.HardwareDirectories(); err == nil {
		ctx.HardwareFolders = packagesFolder
	} else {
		formatter.PrintError(err, "Cannot get hardware directories.")
		os.Exit(commands.ErrCoreConfig)
	}

	if toolsFolder, err := configs.BundleToolsDirectories(); err == nil {
		ctx.ToolsFolders = toolsFolder
	} else {
		formatter.PrintError(err, "Cannot get bundled tools directories.")
		os.Exit(commands.ErrCoreConfig)
	}

	librariesFolder, err := configs.LibrariesFolder.Get()
	if err != nil {
		formatter.PrintError(err, "Cannot get libraries folder.")
		os.Exit(commands.ErrCoreConfig)
	}
	ctx.OtherLibrariesFolders = paths.NewPathList(librariesFolder)

	ctx.BuildPath = paths.New(flags.buildPath)
	if ctx.BuildPath.String() != "" {
		err = ctx.BuildPath.MkdirAll()
		if err != nil {
			formatter.PrintError(err, "Cannot create the build folder.")
			os.Exit(commands.ErrBadCall)
		}
	}

	ctx.Verbose = flags.verbose
	ctx.DebugLevel = flags.debugLevel

	ctx.CoreBuildCachePath = paths.TempDir().Join("arduino-core-cache")

	ctx.USBVidPid = flags.vidPid
	ctx.WarningsLevel = flags.warnings

	ctx.CustomBuildProperties = append(flags.buildProperties, "build.warn_data_percentage=75")

	if flags.buildCachePath != "" {
		ctx.BuildCachePath = paths.New(flags.buildCachePath)
		err = ctx.BuildCachePath.MkdirAll()
		if err != nil {
			formatter.PrintError(err, "Cannot create the build cache folder.")
			os.Exit(commands.ErrBadCall)
		}
	}

	// Will be deprecated.
	ctx.ArduinoAPIVersion = "10600"

	// Check if Arduino IDE is installed and get it's libraries location.
	dataFolder, err := configs.ArduinoDataFolder.Get()
	if err != nil {
		formatter.PrintError(err, "Cannot locate arduino data folder.")
		os.Exit(commands.ErrCoreConfig)
	}

	ideProperties, err := properties.Load(filepath.Join(dataFolder, "preferences.txt"))
	if err == nil {
		lastIdeSubProperties := ideProperties.SubTree("last").SubTree("ide")
		// Preferences can contain records from previous IDE versions. Find the latest one.
		var pathVariants []string
		for k := range lastIdeSubProperties {
			if strings.HasSuffix(k, ".hardwarepath") {
				pathVariants = append(pathVariants, k)
			}
		}
		sort.Strings(pathVariants)
		ideHardwarePath := lastIdeSubProperties[pathVariants[len(pathVariants)-1]]
		ideLibrariesPath := filepath.Join(filepath.Dir(ideHardwarePath), "libraries")
		ctx.BuiltInLibrariesFolders = paths.NewPathList(ideLibrariesPath)
	}

	if flags.showProperties {
		err = builder.RunParseHardwareAndDumpBuildProperties(ctx)
	} else if flags.preprocess {
		err = builder.RunPreprocess(ctx)
	} else {
		err = builder.RunBuilder(ctx)
	}

	if err != nil {
		formatter.PrintError(err, "Compilation failed.")
		os.Exit(commands.ErrGeneric)
	}

	// FIXME: Make a function to obtain these info...
	outputPath := ctx.BuildProperties.ExpandPropsInString("{build.path}/{recipe.output.tmp_file}")
	ext := filepath.Ext(outputPath)
	// FIXME: Make a function to produce a better name...
	fqbn = strings.Replace(fqbn, ":", ".", -1)

	// Copy .hex file to sketch folder
	srcHex := paths.New(outputPath)
	dstHex := sketchPath.Join(sketch.Name + "." + fqbn + ext)
	if err = srcHex.CopyTo(dstHex); err != nil {
		formatter.PrintError(err, "Error copying output file.")
		os.Exit(commands.ErrGeneric)
	}

	// Copy .elf file to sketch folder
	srcElf := paths.New(outputPath[:len(outputPath)-3] + "elf")
	dstElf := sketchPath.Join(sketch.Name + "." + fqbn + ".elf")
	if err = srcElf.CopyTo(dstElf); err != nil {
		formatter.PrintError(err, "Error copying elf file.")
		os.Exit(commands.ErrGeneric)
	}
}

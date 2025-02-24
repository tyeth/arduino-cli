// This file is part of arduino-cli.
//
// Copyright 2020 ARDUINO SA (http://www.arduino.cc/)
//
// This software is released under the GNU General Public License version 3,
// which covers the main part of arduino-cli.
// The terms of this license can be found at:
// https://www.gnu.org/licenses/gpl-3.0.en.html
//
// You can be released from the requirements of the above licenses by purchasing
// a commercial license. Buying such a license is mandatory if you want to
// modify or otherwise use the software for commercial activities involving the
// Arduino software without disclosing the source code of your own applications.
// To purchase a commercial license, send an email to license@arduino.cc.

package compilation

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/arduino/arduino-cli/internal/i18n"
	"github.com/arduino/go-paths-helper"
)

var tr = i18n.Tr

// Database keeps track of all the compile commands run by the builder
type Database struct {
	Contents []Command
	File     *paths.Path
}

// Command keeps track of a single run of a compile command
type Command struct {
	Directory string   `json:"directory"`
	Command   string   `json:"command,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
	File      string   `json:"file"`
}

// NewDatabase creates an empty CompilationDatabase
func NewDatabase(filename *paths.Path) *Database {
	return &Database{
		File:     filename,
		Contents: []Command{},
	}
}

// LoadDatabase reads a compilation database from a file
func LoadDatabase(file *paths.Path) (*Database, error) {
	f, err := file.ReadFile()
	if err != nil {
		return nil, err
	}
	res := NewDatabase(file)
	return res, json.Unmarshal(f, &res.Contents)
}

// SaveToFile save the CompilationDatabase to file as a clangd-compatible compile_commands.json,
// see https://clang.llvm.org/docs/JSONCompilationDatabase.html
func (db *Database) SaveToFile() {
	if jsonContents, err := json.MarshalIndent(db.Contents, "", " "); err != nil {
		fmt.Println(tr("Error serializing compilation database: %s", err))
		return
	} else if err := db.File.WriteFile(jsonContents); err != nil {
		fmt.Println(tr("Error writing compilation database: %s", err))
	}
}

// Add adds a new CompilationDatabase entry
func (db *Database) Add(target *paths.Path, command *paths.Process) {
	commandDir := command.GetDir()
	if commandDir == "" {
		// This mimics what Cmd.Run also does: Use Dir if specified,
		// current directory otherwise
		dir, err := os.Getwd()
		if err != nil {
			fmt.Println(tr("Error getting current directory for compilation database: %s", err))
		}
		commandDir = dir
	}

	entry := Command{
		Directory: commandDir,
		Arguments: command.GetArgs(),
		File:      target.String(),
	}

	db.Contents = append(db.Contents, entry)
}

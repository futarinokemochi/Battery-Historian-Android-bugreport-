// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Setup downloads needed Closure files and generates optimized JS files.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/google/battery-historian/bugreportutils"
	"github.com/google/battery-historian/historianutils"
)

const (
	closureCompilerZip = "compiler-20160208.zip"
	closureCompilerURL = "http://dl.google.com/closure-compiler/" + closureCompilerZip

	thirdPartyDir = "third_party"
)

var rebuild = flag.Bool("rebuild", false, "Whether or not clear all setup files and start from scratch.")

// runCommand runs the given command and only prints the output or error if they're not empty.
func runCommand(name string, args ...string) {
	out, err := historianutils.RunCommand(name, args...)
	if err != nil {
		fmt.Println(err)
	}
	if out != "" {
		fmt.Println(out)
	}
}

// saveFile saves the given contents to the path. relPath must point directly to the file to write to.
func saveFile(relPath string, contents []byte) error {
	f, err := os.Create(relPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, bytes.NewReader(contents))
	return err
}

func deletePath(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Path doesn't exist. Nothing to delete.
		return nil
	}
	if runtime.GOOS == "windows" {
		// os.RemoveAll won't remove read-only files (eg. .git files) on Windows.
		// Modify the permissions path to be writable on Windows.
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chmod(p, 0777)
		})
	}
	return os.RemoveAll(path)
}

func main() {
	flag.Parse()

	if *rebuild {
		fmt.Println("\nClearing files...")
		if err := deletePath(thirdPartyDir); err != nil {
			fmt.Printf("Failed to delete third_party directory: %v\n", err)
			return
		}
		if err := deletePath("compiled"); err != nil {
			fmt.Printf("Failed to delete compiled directory: %v\n", err)
			return
		}
	}

	os.Mkdir(thirdPartyDir, 0777)
	os.Mkdir("compiled", 0777)

	closureLibraryDir := path.Join(thirdPartyDir, "closure-library")
	closureCompilerDir := path.Join(thirdPartyDir, "closure-compiler")
	axisDir := path.Join(thirdPartyDir, "flot-axislabels")

	if _, err := os.Stat(closureLibraryDir); os.IsNotExist(err) {
		fmt.Println("\nDownloading Closure library...")
		runCommand("git", "clone", "https://github.com/google/closure-library", closureLibraryDir)
	}

	_, errD := os.Stat(closureCompilerDir)
	_, errF := os.Stat(path.Join(closureCompilerDir, closureCompilerZip))
	if os.IsNotExist(errD) || os.IsNotExist(errF) {
		fmt.Println("\nDownloading Closure compiler...")
		// Current compiler, if any, is not current. Remove old files.
		if err := deletePath(closureCompilerDir); err != nil {
			fmt.Printf("Failed to clear compiler directory: %v\n", err)
		}
		// Download desired file.
		os.Mkdir(closureCompilerDir, 0777)

		resp, err := http.Get(closureCompilerURL)
		defer resp.Body.Close()

		contents, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Couldn't get zip contents: %v\n", err)
			return
		}

		if err := saveFile(path.Join(closureCompilerDir, closureCompilerZip), contents); err != nil {
			fmt.Printf("Couldn't save Closure zip file: %v\n", err)
			return
		}

		files, err := bugreportutils.Contents("", contents)
		if err != nil {
			fmt.Printf("Couldn't get Closure zip contents: %v\n", err)
			return
		}
		for n, c := range files {
			name := n[1:]
			if err := saveFile(path.Join(closureCompilerDir, name), c); err != nil {
				fmt.Printf("Couldn't save Closure content file: %v\n", err)
				return
			}
		}
	}

	if _, err := os.Stat(axisDir); os.IsNotExist(err) {
		fmt.Println("\nDownloading 3rd-party JS files...")
		runCommand("git", "clone", "https://github.com/xuanluo/flot-axislabels.git", axisDir)
	}

	fmt.Println("\nGenerating JS runfiles...")
	out, err := historianutils.RunCommand("python",
		path.Join(closureLibraryDir, "closure/bin/build/depswriter.py"),
		fmt.Sprintf(`--root=%s`, path.Join(closureLibraryDir, "closure", "goog")),
		`--root_with_prefix=js ../../../../js`)
	if err != nil {
		fmt.Printf("Couldn't generate runfile: %v\n", err)
		return
	}
	if err = saveFile("compiled/historian_deps-runfiles.js", []byte(out)); err != nil {
		fmt.Printf("Couldn't save runfiles file: %v\n", err)
		return
	}

	fmt.Println("\nGenerating optimized JS runfiles...")
	runCommand("java", "-jar",
		path.Join(closureCompilerDir, "compiler.jar"),
		"--closure_entry_point", "historian.upload",
		"--js", "js/*.js",
		"--js", path.Join(closureLibraryDir, "closure/goog/base.js"),
		"--js", path.Join(closureLibraryDir, "closure/goog/**/*.js"),
		"--only_closure_dependencies",
		"--generate_exports",
		"--js_output_file", "compiled/historian-optimized.js",
		"--output_manifest", "compiled/manifest.MF",
		"--compilation_level", "SIMPLE_OPTIMIZATIONS",
	)
}
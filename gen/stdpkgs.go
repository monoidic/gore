// This file is part of GoRE.
//
// Copyright (C) 2019-2024 GoRE Authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

func generateStdPkgs() {
	wg := &sync.WaitGroup{}
	collect := func(ctx context.Context, cause context.CancelCauseFunc, tag string, result chan []string) {
		tree, _, err := githubClient.Git.GetTree(ctx, "golang", "go", tag, true)
		if err != nil {
			cause(fmt.Errorf("error when getting tree for tag %s: %w", tag, err))
			return
		}

		fmt.Println("Fetched std pkgs for tag:", tag)

		if len(tree.Entries) == 100000 {
			fmt.Printf("Warning: tree %s has 100000 entries, this may be limited by api, some might be missing", tag)
		}

		var stdPkgs []string

		for _, entry := range tree.Entries {
			if *entry.Type != "tree" {
				continue
			}

			if !strings.HasPrefix(entry.GetPath(), "src/") ||
				strings.HasPrefix(entry.GetPath(), "src/cmd") ||
				strings.HasSuffix(entry.GetPath(), "_asm") ||
				strings.Contains(entry.GetPath(), "/testdata") {
				continue
			}

			stdPkgs = append(stdPkgs, strings.TrimPrefix(entry.GetPath(), "src/"))
		}
		result <- stdPkgs
		wg.Done()
	}

	f, err := os.OpenFile(goversionCsv, os.O_CREATE|os.O_RDWR, 0664)
	if err != nil {
		fmt.Println("Error when opening goversions.csv:", err)
		return
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	knownVersions, err := getCsvStoredGoversions(f)
	wg.Add(len(knownVersions))

	stdpkgsSet := map[string]struct{}{}

	ctx, cause := context.WithCancelCause(context.Background())
	pkgsChan := make(chan []string)

	for tag := range knownVersions {
		go collect(ctx, cause, tag, pkgsChan)
	}

	go func() {
		wg.Wait()
		close(pkgsChan)
	}()

	for pkgs := range pkgsChan {
		for _, pkg := range pkgs {
			stdpkgsSet[pkg] = struct{}{}
		}
	}

	pkgs := make([]string, 0, len(stdpkgsSet))
	for pkg := range stdpkgsSet {
		pkgs = append(pkgs, pkg)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i] < pkgs[j]
	})

	// Generate the code.
	buf := bytes.NewBuffer(nil)

	err = packageTemplate.Execute(buf, struct {
		Timestamp time.Time
		StdPkg    []string
	}{
		Timestamp: time.Now().UTC(),
		StdPkg:    pkgs,
	})
	if err != nil {
		fmt.Println("Error when generating the code:", err)
		return
	}

	writeOnDemand(buf.Bytes(), outputFile)
}

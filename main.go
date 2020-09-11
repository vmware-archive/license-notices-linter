// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: BSD-2-Clause

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-enry/go-enry/v2"
	ignore "github.com/sabhiram/go-gitignore"
)

var (
	update  = flag.Bool("w", false, "Update files in place")
	verbose = flag.Bool("v", false, "Verbose")
)

var (
	commentPrefixMap = map[string]string{
		"Go": "//",
	}
	errUnknownLanguage = fmt.Errorf("unknown language")
)

var (
	ignorePreds = []func(string) bool{
		enry.IsConfiguration,
		enry.IsDocumentation,
		enry.IsDotFile,
		enry.IsImage,
		enry.IsVendor,
		func(path string) bool {
			b, err := ioutil.ReadFile(path)
			if err != nil {
				panic(err)
			}
			return enry.IsBinary(b)
		},
	}
)

func commentPrefix(filename string) (string, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}
	lang := enry.GetLanguage(filename, b)

	p, found := commentPrefixMap[lang]
	if !found {
		return "", fmt.Errorf("%w %q for %q", errUnknownLanguage, lang, filename)
	}
	return p, nil
}

func crawlFiles(dir string) (res []string, err error) {
	err = filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(path) == ".git" {
				return filepath.SkipDir
			}
			if !info.IsDir() {
				res = append(res, path)
			}
			return nil
		})
	return res, err
}

func ignoreFile(path string, preds ...func(string) bool) bool {
	for _, f := range preds {
		if f(path) {
			return true
		}
	}
	return false
}

type file struct {
	path          string
	commentPrefix string
	copyright     string
	license       string
}

func parseFile(path string) (file, error) {
	pfx, err := commentPrefix(path)
	if err != nil {
		return file{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return file{}, err
	}
	defer f.Close()

	lines, err := head(f, 5)
	if err != nil {
		return file{}, err
	}

	res := file{
		path:          path,
		commentPrefix: pfx,
	}

	for _, l := range lines {
		if strings.HasPrefix(l, fmt.Sprintf("%s Copyright ", pfx)) {
			res.copyright = l[len(pfx)+1:]
		}
		if strings.HasPrefix(l, fmt.Sprintf("%s SPDX-License-Identifier: ", pfx)) {
			res.license = l[len(pfx)+1:]
		}
	}

	return res, nil
}

// head returns up to the first n lines of a reader.
func head(r io.Reader, n int) (res []string, err error) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		res = append(res, strings.TrimRight(line, " \t\r\n"))
	}
	return res, err
}

func mainE() error {
	flag.Parse()

	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	return run(dir, *update, *verbose)
}

func run(dir string, update, verbose bool) error {
	gi, err := ignore.CompileIgnoreFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return err
	}

	gimatch := func(path string) bool {
		r, err := filepath.Rel(dir, path)
		if err != nil {
			panic(err)
		}
		return gi.MatchesPath(r)
	}
	preds := append(ignorePreds, gimatch)

	var candidates []file

	allFiles, err := crawlFiles(dir)
	if err != nil {
		return err
	}
	for _, path := range allFiles {
		if ignoreFile(path, preds...) {
			continue
		}

		f, err := parseFile(path)
		if errors.Is(err, errUnknownLanguage) {
			continue
		}
		if err != nil {
			return err
		}
		candidates = append(candidates, f)
	}

	copyrights := map[string]int{}
	licenses := map[string]int{}

	for _, f := range candidates {
		copyrights[f.copyright]++
		licenses[f.license]++
	}
	delete(copyrights, "")
	delete(licenses, "")

	if len(copyrights) == 0 {
		return fmt.Errorf("cannot find any copyright notice in any source file")
	}
	if len(licenses) == 0 {
		return fmt.Errorf("cannot find any SPDX-License-Identifier tag in any source file")
	}

	top := func(m map[string]int) string { return sortMapDesc(m)[0] }

	copyright := top(copyrights)
	license := top(licenses)

	commentPrefixes := map[string]int{}
	for _, f := range candidates {
		toUpdate := false

		complain := func(about string, args ...interface{}) {
			toUpdate = true
			if verbose {
				fmt.Fprintf(os.Stderr, "file %q %s\n", f.path, fmt.Sprintf(about, args...))
			}
			commentPrefixes[f.commentPrefix]++
		}

		if f.copyright == "" {
			complain("is missing the copyright notice")
		} else if want, got := copyright, f.copyright; want != got {
			complain("has minority copyright notice: want: %q, got: %q", want, got)
		}
		if f.license == "" {
			complain("is missing the license identifier")
		} else if want, got := license, f.license; want != got {
			complain("has minority license identifier: want: %q, got: %q", want, got)
		}

		if toUpdate {
			fmt.Fprintf(os.Stderr, " M %s\n", f.path)
		}
	}

	if !update {
		if len(commentPrefixes) > 0 {
			fmt.Fprintf(os.Stderr, "\n^^^ These files should contain these comments at the top:\n")
			pfx := top(commentPrefixes)
			fmt.Printf("%s %s\n", pfx, copyright)
			fmt.Printf("%s %s\n", pfx, license)
			fmt.Println()
		}
		return nil
	}
	return nil
}

func sortMapDesc(m map[string]int) (res []string) {
	for k := range m {
		if k != "" {
			res = append(res, k)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return m[res[i]] > m[res[j]]
	})
	return res
}

func main() {
	if err := mainE(); err != nil {
		log.Fatal(err)
	}
}

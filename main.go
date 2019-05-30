package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"sort"
	"strings"
)

var localPkg = flag.String("local", "", "local package name")
var wflag = flag.Bool("w", false, "write result to (source) file instead of stdout")
var versionflag = flag.Bool("version", false, "print version")
var verbose = flag.Bool("v", false, "print version")

const version = "gogimport v0.0.2"

func main() {
	if err := fmtMain(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
}

func fmtMain() error {
	var (
		err error
		out *bytes.Buffer
	)

	flag.Parse()
	if len(*localPkg) <= 0 {
		flag.Usage()
		return fmt.Errorf("local must set")
	}

	err = initStdPkg()
	if err != nil {
		return fmt.Errorf("init std package failed: %v", err)
	}

	files := flag.Args()
	if len(files) <= 0 {
		out, err = formatFromIO("stdin", os.Stdin)
		if err != nil {
			return fmt.Errorf("format failed: %v", err)
		}
		fmt.Printf("%s", out)
		return nil
	}

	for _, filename := range files {
		err = formatFile(filename)
		if err != nil {
			return fmt.Errorf("format failed: %v", err)
		}
	}
	return nil
}

func formatFile(filename string) error {
	file, err := os.OpenFile(filename, os.O_RDWR, 644)
	if err != nil {
		return err
	}
	defer file.Close()

	buf, err := formatFromIO(filename, file)
	if err != nil {
		return err
	}

	if *wflag {
		_, err = file.Seek(0, 0)
		if err != nil {
			return err
		}
		err = file.Truncate(int64(buf.Len()))
		if err != nil {
			return err
		}
		_, err = buf.WriteTo(file)
		return err
	}
	fmt.Printf("%s", buf)
	return nil
}

func formatFromIO(filename string, rd io.Reader) (*bytes.Buffer, error) {
	st := &Sorter{
		filename: filename,
		localPkg: *localPkg,
	}

	if *verbose {
		log.Printf("sort import from %s", filename)
	}

	err := st.init(rd)
	if err != nil {
		return nil, err
	}

	st.sortImports()
	return st.toBuffer()
}

func (st *Sorter) sortImports() {
	for _, d := range st.f.Decls {
		d, ok := d.(*ast.GenDecl)
		if !ok || d.Tok != token.IMPORT {
			break
		}

		if !d.Lparen.IsValid() {
			continue
		}
		cf := st.fset.File(st.f.Pos())
		loffset := cf.Position(d.Lparen).Offset
		roffset := cf.Position(d.Rparen).Offset
		st.lines = deleteLinesRange(st.lines, loffset, roffset)
		i := 0
		specs := d.Specs[:0]
		for j, s := range d.Specs {
			if j > i && st.fset.Position(s.Pos()).Line > 1+st.fset.Position(d.Specs[j-1].End()).Line {
				specs = append(specs, st.sortSpecs(d.Specs[i:j])...)
				i = j
			}
		}
		specs = append(specs, st.sortSpecs(d.Specs[i:])...)
		d.Specs = specs
	}
}

func deleteLinesRange(lines []int, lline, rline int) []int {
	nline := []int{}
	for _, line := range lines {
		if line >= lline && line <= rline {
			continue
		}
		nline = append(nline, line)
	}
	return nline
}

func (st *Sorter) sortSpecs(specs []ast.Spec) (results []ast.Spec) {
	innerPkg := []*ast.ImportSpec{}
	thirdpartyPkg := []*ast.ImportSpec{}
	appPkg := []*ast.ImportSpec{}

	lowestPos := token.Pos(MaxInt)
	for _, spec := range specs {
		switch im := spec.(type) {
		case *ast.ImportSpec:
			switch {
			case strings.HasPrefix(im.Path.Value, `"`+st.localPkg):
				if *verbose {
					log.Printf("#$ %s", im.Path.Value)
				}
				appPkg = append(appPkg, im)
			case stdPkgs[strings.Trim(im.Path.Value, `"`)]:
				if *verbose {
					log.Printf("#& %s", im.Path.Value)
				}
				innerPkg = append(innerPkg, im)
			default:
				if *verbose {
					log.Printf("#@ %s", im.Path.Value)
				}
				thirdpartyPkg = append(thirdpartyPkg, im)
			}
			if lowestPos >= im.Pos() {
				lowestPos = im.Pos()
			}
		default:
			if *verbose {
				log.Printf("not importSpec: %#v", im)
			}
		}
	}

	cf := st.fset.File(st.f.Pos())
	// inner package first
	for _, im := range innerPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	if len(innerPkg) > 0 {
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
	}

	// local package second
	for _, im := range appPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	if len(appPkg) > 0 {
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
	}

	// third party third
	for _, im := range thirdpartyPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	if len(thirdpartyPkg) > 0 {
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
	}

	// verify validity of lines table
	size := cf.Size()
	for i, offset := range st.lines {
		if i > 0 && offset <= st.lines[i-1] || size <= offset {
			if *verbose {
				log.Printf("set line faile. %d, %d, %d, %d", i, offset, st.lines[i-1], size)
			}
		}
	}

	ok := cf.SetLines(st.lines)
	if !ok {
		if *verbose {
			log.Printf("setlines faile")
		}
	}

	return results
}

func plugPos(p *token.Pos) token.Pos {
	*p = *p + 1
	return *p
}

func (st *Sorter) init(file io.Reader) error {
	var err error

	src, err := ioutil.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read from %s error: %v", st.filename, err)
	}
	src = append(src, []byte("\n\n\n")...)

	st.lines, err = getLines(src)
	if err != nil {
		return err
	}

	parserMode := parser.ParseComments

	st.fset = token.NewFileSet()
	st.f, err = parser.ParseFile(st.fset, "", src, parserMode)
	if err != nil {
		return fmt.Errorf("parse file error :%s", err.Error())
	}
	return nil
}

func (st *Sorter) toBuffer() (*bytes.Buffer, error) {
	var buf = &bytes.Buffer{}
	if err := format.Node(buf, st.fset, st.f); err != nil {
		return nil, err
	}
	// buf.WriteByte('\n')
	return buf, nil
}

func setPos(pos token.Pos, im *ast.ImportSpec) {
	if im.Name != nil {
		im.Name.NamePos = pos
		// log.Printf("set %v to %d", im.Path.Value, pos)
		im.Path.ValuePos = im.Name.End() + 1
		im.EndPos = im.Path.ValuePos + token.Pos(len(im.Path.Value))
		return
	}

	// log.Printf("set %v to %d", im.Path.Value, pos)
	im.Path.ValuePos = pos
	im.EndPos = im.Path.ValuePos + token.Pos(len(im.Path.Value))
}

func getLines(data []byte) ([]int, error) {
	rd := bufio.NewScanner(bytes.NewBuffer(data))
	offset := 0
	lines := []int{offset}
	for rd.Scan() {
		offset += len(rd.Bytes()) + 1
		lines = append(lines, offset)
	}
	return lines[:len(lines)-1], nil
}

func addline(lines []int, offset ...int) []int {
	lines = append(lines, offset...)
	sort.Ints(lines)
	return deduline(lines)
}

func deduline(lines []int) []int {
	for index, line := range lines {
		if index > 0 && lines[index-1] == line {
			lines = append(lines[0:index-1], lines[index:]...)
		}
	}
	return lines
}

// Sorter gogimport sorter
type Sorter struct {
	filename string
	localPkg string
	lines    []int
	fset     *token.FileSet
	f        *ast.File
}

// interger const
const (
	MaxUint64 = ^uint64(0)
	MinUint64 = 0
	MaxInt64  = int64(MaxUint64 >> 1)
	MinInt64  = -MaxInt64 - 1
	MaxUint   = ^uint(0)
	MinUint   = 0
	MaxInt    = int(MaxUint >> 1)
	MinInt    = -MaxInt - 1
)

var stdPkgs = map[string]bool{}

func initStdPkg() error {
	me, err := user.Current()
	if err != nil {
		return err
	}

	cacheDir := me.HomeDir + "/.gogimport"
	if err = os.MkdirAll(cacheDir, 0666); err != nil {
		return fmt.Errorf("mkdir failed: %v", err)
	}
	cacheFileName := cacheDir + "/" + runtime.Version()
	stat, statErr := os.Stat(cacheFileName)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}

	var reader io.ReadWriter
	cacheFile, err := os.OpenFile(cacheFileName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("open cache file failed: %v", err)
	}
	defer cacheFile.Close()
	if os.IsNotExist(statErr) || stat.Size() < 10 {
		cmd := exec.Command("go", "list", "./...")
		cmd.Dir = strings.TrimSpace(runtime.GOROOT()) + "/src/"
		var stderr bytes.Buffer
		var stdout bytes.Buffer

		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err = cmd.Run(); err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("list standard package failed: %s", stderr.Bytes())
			}
			return fmt.Errorf("list standard package failed: %v", err.Error())
		}
		if err = cacheFile.Truncate(0); err != nil {
			return fmt.Errorf("truncate cache file failed: %v", err)
		}

		reader = &bytes.Buffer{}
		if _, err = stdout.WriteTo(io.MultiWriter(reader, cacheFile)); err != nil {
			return fmt.Errorf("write cache file failed: %v", err)
		}
	} else {
		reader = cacheFile
	}

	// find std packages
	sc := bufio.NewScanner(reader)
	for sc.Scan() {
		stdPkgs[sc.Text()] = true
	}
	return nil
}

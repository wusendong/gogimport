package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
)

const defaultLocal = "code.yunzhanghu.com"

var rootPkg = flag.String("local", "", "local package name")

func main() {
	flag.Parse()
	argIdx := 3
	if *rootPkg == "" {
		*rootPkg = defaultLocal
		argIdx = 1
	}
	files := os.Args[argIdx:]

	for _, fileName := range files {
		formatFile(fileName)
	}
}

func formatFile(fileName string) {
	st := &Sorter{
		filename: fileName,
		rootPkg:  *rootPkg,
	}
	log.Printf("sort import for %s %s", st.rootPkg, st.filename)

	file, err := os.OpenFile(fileName, os.O_RDWR, 644)
	if nil != err {
		log.Printf("open file=%s err=%v", fileName, err)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Printf("close file=%s, err=%v", fileName, err)
		}
	}()

	err = st.init(file)
	if err != nil {
		log.Printf("init error: %v", err.Error())
		return
	}

	st.sortImports()

	err = st.Write(file)
	if err != nil {
		log.Printf("write error %v", err.Error())
		return
	}
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
		specs := d.Specs[:0]
		specs = append(specs, st.sortSpecs(d.Specs[0:])...)
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
			if strings.HasPrefix(im.Path.Value, `"`+st.rootPkg) {
				appPkg = append(appPkg, im)
			} else if isThirdparty(im.Path.Value) {
				thirdpartyPkg = append(thirdpartyPkg, im)
			} else {
				innerPkg = append(innerPkg, im)
			}
			if lowestPos >= im.Pos() {
				lowestPos = im.Pos()
			}
		default:
			log.Printf("default %v", im)
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
			log.Printf("set line faile. %d, %d, %d, %d", i, offset, st.lines[i-1], size)
		}
	}

	ok := cf.SetLines(st.lines)
	if !ok {
		log.Print("setlines failed\n")
	}

	return results
}

func plugPos(p *token.Pos) token.Pos {
	*p = *p + 1
	return *p
}

func (st *Sorter) init(file *os.File) error {
	var err error

	src, err := ioutil.ReadAll(file)
	if err != nil {
		return errors.New("read file " + st.filename + " error: " + err.Error())
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
		log.Printf("parse file error :%s", err.Error())
		return err
	}
	return nil
}

func (st *Sorter) Write(file *os.File) error {
	var buf = &bytes.Buffer{}
	if err := printer.Fprint(buf, st.fset, st.f); err != nil {
		return err
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	_, err = file.Seek(0, 0)
	if err != nil {
		return err
	}
	err = file.Truncate(int64(buf.Len()))
	if err != nil {
		return err
	}
	_, err = file.Write(out)
	return err
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

var thirdPartRegex = regexp.MustCompile(`^"[a-z]+\.[a-z\.]+`)

func isThirdparty(path string) bool {
	return thirdPartRegex.MatchString(path)
}

// Sorter gogimport sorter
type Sorter struct {
	filename string
	rootPkg  string
	lines    []int
	fset     *token.FileSet
	f        *ast.File
}

// integer const
const (
	MaxUint64 = ^uint64(0)
	MaxInt64  = int64(MaxUint64 >> 1)
	MaxUint   = ^uint(0)
	MaxInt    = int(MaxUint >> 1)
)

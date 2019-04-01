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
	"sort"
	"strings"
)

var rootPkg = flag.String("local", "", "local package name")

func main() {
	flag.Parse()
	if len(*rootPkg) <= 0 {
		flag.Usage()
		log.Fatalln("local must set")
	}
	files := os.Args[3:]

	for _, filename := range files {
		sortFile(filename)
	}
}

func sortFile(filename string) {
	st := &Sorter{
		filename: filename,
		rootPkg:  *rootPkg,
	}
	log.Printf("sort import for %s %s", st.rootPkg, st.filename)

	file, err := os.OpenFile(filename, os.O_RDWR, 644)
	if nil != err {
		log.Print("open file " + filename + " error: " + err.Error())
	}
	defer file.Close()

	err = st.init(file)
	if err != nil {
		log.Printf("init error: %s", err.Error())
		return
	}

	st.sortImports()

	err = st.Write(file)
	if err != nil {
		log.Printf("write error %s", err.Error())
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
			if strings.HasPrefix(im.Path.Value, `"`+st.rootPkg) {
				appPkg = append(appPkg, im)
			} else if isThirparty(im.Path.Value) {
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
		log.Printf("setlines faile")
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

func isThirparty(path string) bool {
	for _, pkg := range thirdpartyPrefix {
		if strings.HasPrefix(path, `"`+pkg) {
			return true
		}
	}
	return false
}

var thirdpartyPrefix = []string{
	"github",
	"gitlab",
	"gopkg",
}

// Sorter gogimport sorter
type Sorter struct {
	filename string
	rootPkg  string
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

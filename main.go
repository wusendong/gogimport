package main

import (
	"bufio"
	"bytes"
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

var rootPkg = flag.String("pkg", "", "sort package")

func main() {
	flag.Parse()
	if len(*rootPkg) <= 0 {
		flag.Usage()
		log.Fatalln("pkg must set")
	}
	files := os.Args[3:]

	for _, file := range files {
		st := &Sorter{
			filename: file,
			rootPkg:  *rootPkg,
		}
		log.Printf("sort import for %s %s", st.rootPkg, st.filename)

		err := st.Init()
		if err != nil {
			log.Printf("init error %s", err.Error())
			continue
		}

		st.sortImports()

		err = st.Write()
		if err != nil {
			log.Printf("write error %s", err.Error())
			continue
		}
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

	lowestPos := token.Pos(9999999999)
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
	for _, im := range innerPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
	for _, im := range thirdpartyPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
	for _, im := range appPkg {
		results = append(results, im)
		lenth := im.End() - im.Pos()
		setPos(lowestPos, im)
		lowestPos += lenth
		st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)
		lowestPos++
	}
	st.lines = addline(st.lines, cf.Position(plugPos(&lowestPos)).Offset)

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

func (st *Sorter) Init() error {
	var err error
	st.lines, err = getLines(st.filename)
	if err != nil {
		log.Printf("get file lines error %s", err.Error())
		return err
	}

	st.fset = token.NewFileSet()
	st.f, err = parser.ParseFile(st.fset, st.filename, nil, 0)
	if err != nil {
		log.Printf("parse file error %s", err.Error())
		return err
	}
	return nil
}

func (st *Sorter) Write() error {

	var buf = &bytes.Buffer{}
	if err := (&printer.Config{Tabwidth: 8}).Fprint(buf, st.fset, st.f); err != nil {
		return err
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	return ioutil.WriteFile(st.filename, out, 644)
}

func setPos(pos token.Pos, im *ast.ImportSpec) {
	if im.Name != nil {
		im.Name.NamePos = pos
		// log.Printf("set %v to %d", im.Path.Value, pos)
		im.Path.ValuePos = im.Name.End() + 1
		im.EndPos = im.Path.ValuePos + token.Pos(len(im.Path.Value))
		return
	}

	log.Printf("set %v to %d", im.Path.Value, pos)
	im.Path.ValuePos = pos
	im.EndPos = im.Path.ValuePos + token.Pos(len(im.Path.Value))
}

func getLines(filename string) ([]int, error) {
	file, err := os.Open(filename)
	if nil != err {
		return nil, err
	}
	defer file.Close()
	rd := bufio.NewScanner(file)
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

type Sorter struct {
	filename string
	rootPkg  string
	lines    []int
	fset     *token.FileSet
	f        *ast.File
}

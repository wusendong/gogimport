package main

import (
	"bufio"
	"bytes"
	"container/list"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
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

var rootPkg = flag.String("local", "", "local package name")
var vflag = flag.Bool("version", false, "print version")

const version = "gogimport v0.0.1"

var stdPkgs = map[string]bool{}

func main() {
	flag.Parse()
	if *vflag {
		fmt.Println(version)
	}
	if len(*rootPkg) <= 0 {
		flag.Usage()
		log.Fatalln("local must set")
	}

	err := initStdPkg()
	if err != nil {
		log.Fatalf("init std package failed: %v", err)
	}

	files := os.Args[3:]
	for _, filename := range files {
		sortFile(filename)
	}
}

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

// func (st *Sorter) init(file *os.File) error {
// 	var err error

// 	src, err := ioutil.ReadAll(file)
// 	if err != nil {
// 		return errors.New("read file " + st.filename + " error: " + err.Error())
// 	}
// 	src = append(src, []byte("\n\n\n")...)

// 	st.lines, err = getLines(src)
// 	if err != nil {
// 		return err
// 	}

// 	parserMode := parser.ParseComments

// 	st.fset = token.NewFileSet()
// 	st.f, err = parser.ParseFile(st.fset, "", src, parserMode)
// 	if err != nil {
// 		log.Printf("parse file error :%s", err.Error())
// 		return err
// 	}
// 	return nil
// }

// func (st *Sorter) Write(file *os.File) error {
// 	var buf = &bytes.Buffer{}
// 	if err := printer.Fprint(buf, st.fset, st.f); err != nil {
// 		return err
// 	}
// 	out, err := format.Source(buf.Bytes())
// 	if err != nil {
// 		return err
// 	}
// 	_, err = file.Seek(0, 0)
// 	if err != nil {
// 		return err
// 	}
// 	err = file.Truncate(int64(buf.Len()))
// 	if err != nil {
// 		return err
// 	}
// 	_, err = file.Write(out)
// 	return err
// }

var importOnePrefix = []byte("import")
var importMutiPrefix = []byte("import (")
var importEnd = []byte{')'}

func sortFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lines := scannFileLines(file)
	dealLines(lines)
	buf := linesToBuffer(lines)

	return format.Source(buf.Bytes())
}

func sortFromIOl()

func scannFileLines(file *os.File) *list.List {
	rd := bufio.NewScanner(file)
	l := list.New()
	for rd.Scan() {
		l.PushBack(rd.Bytes())
	}
	return l
}

func dealLines(lines *list.List) {
	var next = lines.Front()
	for {
		next = findImportLine(lines.Front())
		if next == nil {
			break
		}
		next = sortImport(lines, next.Next(), "")
		if next == nil {
			break
		}
	}

}

func findImportLine(head *list.Element) *list.Element {
	for head != nil && !bytes.HasPrefix(head.Value.([]byte), importMutiPrefix) {
		head = head.Next()
	}
	return head
}

var newline = []byte{'\n'}

func sortImport(lines *list.List, head *list.Element, localPrefix string) *list.Element {
	std := lines.InsertBefore(newline, head)
	local := lines.InsertBefore(newline, head)
	thirdparty := lines.InsertBefore(newline, head)

	var next *list.Element
	for ; head != nil && !bytes.HasPrefix(head.Value.([]byte), importEnd); head = next {
		next = head.Next()
		pkg := getImportPkg(head.Value.([]byte))
		switch {
		case stdPkgs[pkg]:
			lines.MoveBefore(head, std)
		case strings.HasPrefix(pkg, localPrefix):
			lines.MoveBefore(head, local)
		default:
			lines.MoveBefore(head, thirdparty)
		}
	}
	return head

}

func getImportPkg(a []byte) string {
	index := bytes.Index(a, []byte{'"'})
	if index > 0 {
		return string(bytes.Trim(bytes.TrimSpace(a[index:]), `"`))
	}
	return string(bytes.Trim(bytes.TrimSpace(a), `"`))
}

func linesToBuffer(lines *list.List) *bytes.Buffer {
	head := lines.Front()
	buf := bytes.Buffer{}
	for head != nil {
		buf.Write(head.Value.([]byte))
		head = head.Next()
		if head == nil {
			break
		}
		buf.Write([]byte{'\n'})
	}
	return &buf
}

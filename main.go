package main

import (
	"bufio"
	"bytes"
	"container/list"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

var localPkg = flag.String("local", "", "local package name")
var wflag = flag.Bool("w", false, "write result to (source) file instead of stdout")
var vflag = flag.Bool("version", false, "print version")

const version = "gogimport v0.0.2"

var stdPkgs = map[string]bool{}

func main() {
	if err := fmtMain(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
}

func fmtMain() error {
	var (
		err error
		out []byte
	)

	flag.Parse()
	if *vflag {
		fmt.Println(version)
		return nil
	}

	err = initStdPkg()
	if err != nil {
		return fmt.Errorf("init std package failed: %v", err)
	}

	files := flag.Args()
	if len(files) <= 0 {
		out, err = formatFromStdin()
		if err != nil {
			return fmt.Errorf("format failed: %v", err)
		}
		fmt.Printf("%s", out)
		return nil
	}
	for _, filename := range files {
		out, err = formatFile(filename)
		if err != nil {
			return fmt.Errorf("format failed: %v", err)
		}
		if *wflag {
			if err = ioutil.WriteFile(filename, out, 0); err != nil {
				return fmt.Errorf("rewrite file failed: %v", err)
			}
		} else {
			fmt.Printf("%s", out)
			return nil
		}
	}
	return nil
}

var importOnePrefix = []byte("import")
var importMutiPrefix = []byte("import (")
var importEnd = []byte{')'}

func formatFromStdin() ([]byte, error) {
	return formatFromIO(os.Stdin)
}

func formatFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return formatFromIO(file)
}

func formatFromIO(rd io.Reader) ([]byte, error) {
	lines, err := scannFileLines(rd)
	if err != nil {
		return nil, err
	}
	formatLines(lines)
	buf := linesToBuffer(lines)
	buf.Write([]byte{'\n'})
	return buf.Bytes(), nil
}

func scannFileLines(file io.Reader) (*list.List, error) {
	content, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	preproccess, err := format.Source(content)
	if err != nil {
		return nil, err
	}

	rd := bufio.NewScanner(bytes.NewBuffer(preproccess))
	l := list.New()
	for rd.Scan() {
		l.PushBack(rd.Bytes())
	}
	return l, nil
}

func formatLines(lines *list.List) {
	var next = lines.Front()
	for {
		next = findImportLine(next)
		if next == nil {
			break
		}
		next = sortImport(lines, next.Next(), *localPkg)
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

var newline = []byte{}

func sortImport(lines *list.List, head *list.Element, localPrefix string) *list.Element {
	// insert placeholder
	std := lines.InsertBefore(newline, head)
	thirdparty := lines.InsertBefore(newline, head)
	local := lines.InsertBefore(newline, head)

	// sort imports
	var hasStd, hasLocal, hasThirdParty bool
	var cur, next = head, head.Next()
	for ; cur != nil && !bytes.HasPrefix(cur.Value.([]byte), importEnd); cur = next {
		// fmt.Printf("\n***********\n%s\n********\n\n", linesToBuffer(lines))
		next = cur.Next()
		pkg := getImportPkg(cur.Value.([]byte))
		if pkg == "" {
			// fmt.Printf("#!%s\n", cur.Value)
			lines.Remove(cur)
			continue
		}
		switch {
		case strings.HasPrefix(pkg, localPrefix):
			hasLocal = true
			insertSort(lines, local, cur)
		case stdPkgs[pkg]:
			hasStd = true
			insertSort(lines, std, cur)
			// fmt.Printf("#$%s", cur.Value)
			// fmt.Printf("#&%s", cur.Value)
		default:
			hasThirdParty = true
			insertSort(lines, thirdparty, cur)
			// fmt.Printf("#@%s", cur.Value)
		}
		// fmt.Printf("\t pkg: %s\n", pkg)
	}

	// remove placeholder that does'n needed
	lines.Remove(local)
	if (!hasLocal && hasThirdParty) || !hasThirdParty {
		lines.Remove(local)
	}
	if (!hasThirdParty && !hasLocal && hasStd) || !hasStd {
		lines.Remove(std)
	}

	return cur
}

func insertSort(lines *list.List, head *list.Element, e *list.Element) {
	for ; head != nil; head = head.Prev() {
		if bytes.Compare(head.Prev().Value.([]byte), importMutiPrefix) != 0 {
			break
		}
		if len(head.Prev().Value.([]byte)) > 0 {
			break
		}
		if importLessThan(e.Value.([]byte), head.Value.([]byte)) {
			break
		}
	}
	lines.MoveBefore(e, head)
}

func importLessThan(a, b []byte) bool {
	return getImportPkg(a) < getImportPkg(b)
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

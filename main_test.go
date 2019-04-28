package main

import (
	"container/list"
	"reflect"
	"strings"
	"testing"
	"time"
)

func Test_getImportPkg(t *testing.T) {
	type args struct {
		a []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"",
			args{[]byte(`alias "byte"`)},
			`byte`,
		},
		{
			"",
			args{[]byte(`alias byte`)},
			`alias byte`,
		},
		{
			"",
			args{[]byte(`"byte"`)},
			`byte`,
		},
		{
			"",
			args{[]byte(`	"byte"`)},
			`byte`,
		},
		{
			"",
			args{[]byte(`byte`)},
			`byte`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getImportPkg(tt.args.a); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getImportPkg() = %s, want %s", got, tt.want)
			}
		})
	}
}

func Test_sortLinkedList(t *testing.T) {
	var localPrefix = "gogimport"

	err := initStdPkg()
	if err != nil {
		t.Fatalf("init std package failed: %v", err)
	}

	var buildLineList = func(content string) *list.List {
		l := list.New()
		for _, line := range strings.Split(content, "\n") {
			l.PushBack([]byte(line))
		}

		return l
	}

	go func() {
		time.AfterFunc(time.Second*11, func() { panic("ot") })
	}()

	l2 := buildLineList(`package

import (
	"github.com/sirusen/barabra"
	"io"
	"gogimport/pkg1"
	"gogimport/pkg2"
)`)

	type args struct {
		lines *list.List
		head  *list.Element
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"",
			args{
				l2,
				l2.Front().Next().Next().Next(),
			},
			`package

import (
	"io"


	"gogimport/pkg1"
	"gogimport/pkg2"


	"github.com/sirusen/barabra"


)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if sortImport(tt.args.lines, tt.args.head, localPrefix); listToBuffer(tt.args.lines.Front()).String() != tt.want {
				t.Errorf("sortLinkedList() = %v, want %v", listToBuffer(tt.args.lines.Front()).String(), tt.want)
			}
		})
	}
}

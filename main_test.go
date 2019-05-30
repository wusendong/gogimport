package main

import (
	"bytes"
	"reflect"
	"testing"

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

func Test_formatFromIO(t *testing.T) {
	var localPkgPrefix = "gogimport"
	localPkg = &localPkgPrefix

	err := initStdPkg()
	if err != nil {
		t.Fatalf("init std package failed: %v", err)
	}

	tests := []struct {
		name    string
		args    string
		want    string
		wantErr bool
	}{
		{
			"",
			`package main

import (
	"io"

	"github.com/sirusen/barabra"
	"gogimport/pkg1"
	"gogimport/pkg2"

)`,
			`package main

import (
	"io"

	"github.com/sirusen/barabra"

	"gogimport/pkg1"
	"gogimport/pkg2"
)
`,
			false},
		{
			"",
			`package main

import (
	"io"
	f "fmt"
	"net/http"

	"github.com/sirusen/barabra"
	k "gogimport/a/b"
	"gogimport/a"
	"gogimport/a/b/c"

)`,
			`package main

import (
	f "fmt"
	"io"
	"net/http"

	"github.com/sirusen/barabra"

	"gogimport/a"
	k "gogimport/a/b"
	"gogimport/a/b/c"
)
`,
			false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatFromIO(bytes.NewBufferString(tt.args))
			if (err != nil) != tt.wantErr {
				t.Errorf("formatFromIO() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf(`
got  %q, 
want %q`, got, tt.want)
			}
		})
	}
}

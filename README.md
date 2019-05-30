## gogimport
golang grouping import tool

### introduction

gogimport will grouping imports by stdlib, thirdparty, local packages like the example below,
unlike what [goimports](https://godoc.org/golang.org/x/tools/cmd/goimports) did,  we prefer to do it this way:

- do not remove unuse import, is useful when our codes is in development.
- force grouping into three groups, this reduce the changed lines so tha make code review simple.

```sh
$ gogimport -local github.com/wusendong/example main.go
```

```go
package main

import (
    "fmt"
    "log"

    "gopkg.in/redis.v5"
    "github.com/gorilla/context"

    "github.com/wusendong/example"
)

```


### install
```
go get -u github.com/wusendong/gogimport
```


### usage
```
Usage of gogimport:
gogimport [options] [file ...]

Options:
  -local string
        local package name
Example command:
gogimport -local ${packaname} some.go other.go
```

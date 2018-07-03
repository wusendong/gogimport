## gogimport
golang grouping import tool

### introduction

gogimport will grouping imports by stdlib, thirdparty, custom packages like the example below:


```sh
$ gogimport -pkg github.com/wusendong/example main.go
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


### usage
```
Usage of gogimport:
  -pkg string
        custom package
```

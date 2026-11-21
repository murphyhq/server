package telemetry

import (
	"runtime"
	"strings"
)

type Caller struct {
	Package  string
	Function string
}

var receiver = strings.NewReplacer("(*", "", ")", "")

func CallerOf(skip int) Caller {
	pc, _, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return Caller{Package: "unknown", Function: "unknown"}
	}
	full := runtime.FuncForPC(pc).Name()

	slash := strings.LastIndex(full, "/")
	dot := strings.Index(full[slash+1:], ".")
	if dot < 0 {
		return Caller{Package: full, Function: full}
	}
	pkg := full[:slash+1+dot]
	function := receiver.Replace(full[slash+1:])
	return Caller{Package: pkg, Function: function}
}

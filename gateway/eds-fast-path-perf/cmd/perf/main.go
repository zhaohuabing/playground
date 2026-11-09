// Command perf measures how long an EndpointSlice change takes to reach Envoy,
// and what the Envoy Gateway control plane spends meanwhile.
//
//	perf run    -mode full|fast -out results/<run>   # one EG mode, all scenarios
//	perf report -dir results/<run>                   # compare full vs fast
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: perf run|report [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runCmd(os.Args[2:])
	case "report":
		err = reportCmd(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

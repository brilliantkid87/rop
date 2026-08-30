// Command ropctl is the ROP CLI (Master Prompt §75). M1 implements inspect,
// plan, reverse, and verify against a running ropd. There is deliberately no
// batch reverse command (Master Prompt §30, §75) and no reconcile command
// until its semantics are understood (§75).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const usage = `ropctl — ROP CLI (experimental)

Usage:
  ropctl [flags] <command> <action-id>

Commands:
  inspect <action-id>   fetch the Action's current receipt/status document
  plan    <action-id>   request a read-only reversal plan (optional capability)
  reverse <action-id>   request reversal (optional capability)
  verify  <action-id>   evaluate provider-defined postconditions

Flags:
`

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "ropd base URL")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(2)
	}
	command, actionID := args[0], args[1]

	var method, path string
	switch command {
	case "inspect":
		method, path = http.MethodGet, "/.well-known/rop/actions/"+actionID
	case "plan":
		method, path = http.MethodPost, "/.well-known/rop/actions/"+actionID+"/plan-reversal"
	case "reverse":
		method, path = http.MethodPost, "/.well-known/rop/actions/"+actionID+"/reverse"
	case "verify":
		method, path = http.MethodGet, "/.well-known/rop/actions/"+actionID+"/verification"
	default:
		fmt.Fprintf(os.Stderr, "ropctl: unknown command %q\n", command)
		flag.Usage()
		os.Exit(2)
	}

	req, err := http.NewRequest(method, strings.TrimRight(*server, "/")+path, nil)
	if err != nil {
		fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fatal(err)
	}

	var pretty map[string]any
	if err := json.Unmarshal(body, &pretty); err != nil {
		fmt.Println(string(body))
	} else {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
	}
	// Semantic outcomes are in the body (transport success is not semantic
	// success, §28): 200 with a problem-shaped body is impossible by
	// construction, but non-2xx always means the request was not fulfilled.
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ropctl:", err)
	os.Exit(1)
}

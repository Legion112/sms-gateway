// Command sms-gateway-scan blocks the operator's own identifiers from being
// committed, pushed or published.
//
// It is also the git hook. Git runs whatever executable sits at
// .git/hooks/pre-commit, and nothing says that has to be a shell script, so
// `make hooks` installs copies of this binary under the hook names and it
// dispatches on the name it was invoked as. One compiled program, no shell.
//
// Modes:
//
//	sms-gateway-scan               scan the working tree
//	sms-gateway-scan FILE...       scan named files
//	sms-gateway-scan -staged       scan what is staged        (pre-commit)
//	sms-gateway-scan -push         scan what is being pushed  (pre-push, stdin)
//	sms-gateway-scan -range A..B   scan a commit range
//	sms-gateway-scan -history      scan every blob in every ref
//	sms-gateway-scan -reserved     print the placeholders that are allowed
//
// It exits 1 when it finds something, 0 when it does not, and 2 when it could
// not look. Exiting non-zero for a failure to look is deliberate: a scanner
// that cannot run must not read as a clean result.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/legion/sms-gateway/internal/secretscan"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Dispatch on the invoked name before parsing flags, because git calls the
	// hooks with arguments of its own that these flags know nothing about.
	switch filepath.Base(os.Args[0]) {
	case "pre-commit":
		return report(scanStaged())
	case "pre-push":
		return report(scanPush(os.Stdin))
	}

	staged := flag.Bool("staged", false, "scan the staged contents")
	push := flag.Bool("push", false, "scan the range given by git pre-push on stdin")
	history := flag.Bool("history", false, "scan every blob reachable from every ref")
	reserved := flag.Bool("reserved", false, "print the reserved placeholder values")
	install := flag.Bool("install-hooks", false, "install this binary as the pre-commit and pre-push hooks")
	rangeSpec := flag.String("range", "", "scan the blobs introduced by a commit range, e.g. origin/master..HEAD")
	flag.Parse()

	switch {
	case *reserved:
		fmt.Print(secretscan.Reserved())
		return 0
	case *install:
		return installHooks()
	case *staged:
		return report(scanStaged())
	case *push:
		return report(scanPush(os.Stdin))
	case *history:
		return report(scanRevList("--all"))
	case *rangeSpec != "":
		return report(scanRevList(*rangeSpec))
	case flag.NArg() > 0:
		return report(scanFiles(flag.Args()))
	default:
		return report(scanTracked())
	}
}

type result struct {
	findings []secretscan.Finding
	scanned  int
	err      error
}

func report(r result) int {
	if r.err != nil {
		fmt.Fprintf(os.Stderr, "sms-gateway-scan: %v\n", r.err)
		return 2
	}
	if len(r.findings) == 0 {
		fmt.Fprintf(os.Stderr, "sms-gateway-scan: %d file(s) scanned, nothing found\n", r.scanned)
		return 0
	}
	fmt.Fprintf(os.Stderr, "sms-gateway-scan: refusing %d suspected identifier(s)\n\n", len(r.findings))
	for _, f := range r.findings {
		fmt.Fprintf(os.Stderr, "  %s\n", f)
	}
	fmt.Fprint(os.Stderr, `
These look like real identifiers rather than placeholders. This repository is
public, and a SIM serial, a subscriber number, a card tail or an account
balance stays sensitive long after a one-time code has expired.

Replace the value with a reserved placeholder:

    sms-gateway-scan -reserved

If a value really is safe and the shape is a false alarm, waive that one line
with a reason, which keeps the argument in the diff:

    ... // secretscan:allow operator hotline, published on their website

`)
	return 1
}

// scanStaged reads what the commit would contain, which is the staged blob and
// not the file on disk: those differ whenever somebody stages a fix and then
// keeps editing, and the commit is what gets published.
func scanStaged() result {
	out, err := git("diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return result{err: err}
	}
	var r result
	for _, path := range splitZ(out) {
		if secretscan.Skip(path) {
			continue
		}
		blob, err := git("show", ":"+path)
		if err != nil {
			// A path can disappear between the two commands during a rebase
			// or a merge; that is not a finding and not a failure.
			continue
		}
		r.scanned++
		r.findings = append(r.findings, secretscan.ScanBytes(path, blob)...)
	}
	return r
}

// scanPush reads git's pre-push stdin, one line per ref being updated:
//
//	<local ref> <local oid> <remote ref> <remote oid>
//
// and scans the objects this push would add. A deletion carries a zero local
// oid and adds nothing. A brand new branch carries a zero remote oid, in which
// case the objects to scan are the ones no other remote-tracking ref already
// has, so that pushing a branch does not re-scan the whole repository.
func scanPush(stdin *os.File) result {
	var r result
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		localOID, remoteOID := fields[1], fields[3]
		if isZeroOID(localOID) {
			continue
		}
		var args []string
		if isZeroOID(remoteOID) {
			args = []string{localOID, "--not", "--remotes"}
		} else {
			args = []string{remoteOID + ".." + localOID}
		}
		sub := scanRevList(args...)
		if sub.err != nil {
			return sub
		}
		r.scanned += sub.scanned
		r.findings = append(r.findings, sub.findings...)
	}
	if err := sc.Err(); err != nil {
		return result{err: err}
	}
	return r
}

// scanRevList scans every blob named by a rev-list argument set. Blobs are
// deduplicated by object id: an unchanged file shared by forty commits is one
// blob and worth reading once.
func scanRevList(revArgs ...string) result {
	args := append([]string{"rev-list", "--objects"}, revArgs...)
	out, err := git(args...)
	if err != nil {
		return result{err: err}
	}
	type blob struct{ sha, path string }
	var queue []blob
	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		sha, path, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || len(sha) != 40 || path == "" || secretscan.Skip(path) {
			continue
		}
		if _, dup := seen[sha]; dup {
			continue
		}
		seen[sha] = struct{}{}
		queue = append(queue, blob{sha, path})
	}

	var r result
	for _, b := range queue {
		kind, err := git("cat-file", "-t", b.sha)
		if err != nil || strings.TrimSpace(string(kind)) != "blob" {
			continue
		}
		data, err := git("cat-file", "blob", b.sha)
		if err != nil {
			continue
		}
		r.scanned++
		// The object id locates a finding that no longer exists at any tip,
		// which is the only handle a history audit can offer.
		r.findings = append(r.findings, secretscan.ScanBytes(b.path+"@"+b.sha[:8], data)...)
	}
	return r
}

func scanTracked() result {
	out, err := git("ls-files", "-z")
	if err != nil {
		return result{err: err}
	}
	return scanFiles(splitZ(out))
}

func scanFiles(paths []string) result {
	var r result
	for _, path := range paths {
		if secretscan.Skip(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		r.scanned++
		r.findings = append(r.findings, secretscan.ScanBytes(path, data)...)
	}
	return r
}

// installHooks copies this binary into the repository's hook directory under
// the hook names it dispatches on.
//
// It copies rather than symlinks because `make clean` removes bin/, and a
// dangling hook makes every commit fail with a message about a missing file
// instead of doing its job. The cost is that a rule change does not reach the
// hooks until `make hooks` runs again, which is why the same scanner also runs
// in CI, where it is the copy that cannot go stale.
func installHooks() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sms-gateway-scan: locating this binary: %v\n", err)
		return 2
	}
	body, err := os.ReadFile(self)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sms-gateway-scan: reading this binary: %v\n", err)
		return 2
	}
	out, err := git("rev-parse", "--git-path", "hooks")
	if err != nil {
		return 2
	}
	dir := strings.TrimSpace(string(out))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sms-gateway-scan: %v\n", err)
		return 2
	}
	for _, name := range []string{"pre-commit", "pre-push"} {
		path := filepath.Join(dir, name)
		// Written to a temporary name and renamed, because overwriting a file
		// that is currently executing fails with ETXTBSY.
		tmp := path + ".new"
		if err := os.WriteFile(tmp, body, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sms-gateway-scan: %v\n", err)
			return 2
		}
		if err := os.Rename(tmp, path); err != nil {
			fmt.Fprintf(os.Stderr, "sms-gateway-scan: %v\n", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "installed %s\n", path)
	}
	fmt.Fprint(os.Stderr, "\nHooks are local to this clone and git does not carry them, so every\nclone needs `make hooks`. `git commit --no-verify` skips them, which is\nwhy the same scanner runs in CI as well.\n")
	return 0
}

func git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func splitZ(b []byte) []string {
	var out []string
	for _, s := range strings.Split(string(b), "\x00") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isZeroOID(s string) bool { return strings.Trim(s, "0") == "" }

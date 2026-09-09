package main

import "flag"

// parseFlags accepts flags before, after or between positional arguments.
// Go's flag package stops at the first non-flag, which silently ignored
// "mdrev list doc.md --json" — the flag looked accepted and did nothing.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

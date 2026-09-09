package main

import "flag"

// parseFlags accepts flags before, after or between positional arguments. Go's
// flag package stops at the first non-flag, so "mdrev list doc.md --json"
// silently ignored the flag.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	// Everything after "--" is literal; re-parsing would forget that.
	for i, a := range args {
		if a == "--" {
			positional, err := parseFlags(fs, args[:i])
			return append(positional, args[i+1:]...), err
		}
	}

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

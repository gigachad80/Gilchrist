
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// --- Main Dispatcher ---
func main() {
	if len(os.Args) < 2 {
		printGeneralHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:] // Arguments for the subcommand

	switch command {
	case "wc":
		handleWc(args)
	case "find":
		handleFind(args)
	case "rm":
		handleRm(args)
	case "help":
		printGeneralHelp()
	default:
		fmt.Fprintf(os.Stderr, "gilchrist: unknown command '%s'\n", command)
		printGeneralHelp()
		os.Exit(1)
	}
}

func printGeneralHelp() {
	fmt.Println("Usage: gilchrist <command> [options] [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  wc    Print newline, word, byte, and character counts")
	fmt.Println("  find  Search for files in a directory hierarchy")
	fmt.Println("  rm    Remove files or directories")
	fmt.Println("\nUse 'gilchrist <command> -h' for command-specific help.")
}

// --- WC Implementation ---

type wcCounts struct {
	lines   int64
	words   int64
	bytes   int64
	chars   int64
	maxLine int64 // For -L flag
}

// processStreamForWc processes an io.Reader (like a file or stdin buffer)
// and updates the counts. It assumes the entire stream content is passed for byte count.
func processStreamForWc(r io.Reader, totalBytes int64, counts *wcCounts) error {
	counts.bytes = totalBytes // Set total bytes from pre-read content or file stat

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lineBytes := scanner.Bytes()    // Get raw bytes of the line (excluding newline)
		lineString := string(lineBytes) // Convert to string for word and char count

		counts.lines++
		counts.words += int64(len(strings.Fields(lineString)))
		counts.chars += int64(utf8.RuneCount(lineBytes)) // Count runes (characters) in the line

		currentLineRuneCount := int64(utf8.RuneCount(lineBytes))
		if currentLineRuneCount > counts.maxLine {
			counts.maxLine = currentLineRuneCount
		}
	}
	return scanner.Err()
}

func printWcLine(counts wcCounts, showL, showW, showC, showM, showMaxL bool, name string) {
	var parts []string
	if showL {
		parts = append(parts, fmt.Sprintf("%8d", counts.lines))
	}
	if showW {
		parts = append(parts, fmt.Sprintf("%8d", counts.words))
	}
	if showM { // GNU wc -m is chars
		parts = append(parts, fmt.Sprintf("%8d", counts.chars))
	}
	if showC { // GNU wc -c is bytes
		parts = append(parts, fmt.Sprintf("%8d", counts.bytes))
	}
	if showMaxL {
		parts = append(parts, fmt.Sprintf("%8d", counts.maxLine))
	}

	if name != "" {
		fmt.Printf("%s %s\n", strings.Join(parts, ""), name)
	} else {
		fmt.Printf("%s\n", strings.Join(parts, ""))
	}
}

func handleWc(args []string) {
	wcCmd := flag.NewFlagSet("wc", flag.ExitOnError)
	showLines := wcCmd.Bool("l", false, "print the newline counts")
	showWords := wcCmd.Bool("w", false, "print the word counts")
	showBytes := wcCmd.Bool("c", false, "print the byte counts")
	showChars := wcCmd.Bool("m", false, "print the character counts")
	showMaxLine := wcCmd.Bool("L", false, "print the maximum display width (character count of longest line)")
	helpFlag := wcCmd.Bool("h", false, "display this help and exit")

	wcCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gilchrist wc [OPTION]... [FILE]...\n")
		fmt.Fprintf(os.Stderr, "Print newline, word, character, byte, and/or maximum line length counts for\n")
		fmt.Fprintf(os.Stderr, "each FILE, and a total line if more than one FILE is specified. With no FILE,\n")
		fmt.Fprintf(os.Stderr, "or when FILE is -, read standard input.\n\n")
		fmt.Fprintf(os.Stderr, "A word is a non-zero-length sequence of characters delimited by white space.\n\n")
		fmt.Fprintf(os.Stderr, "The options below may be used to select which counts are printed, always in\n")
		fmt.Fprintf(os.Stderr, "the following order: newline, word, character, byte, maximum line length.\n")
		wcCmd.PrintDefaults()
	}

	wcCmd.Parse(args) // Supports combined boolean flags like -lwc natively.

	if *helpFlag {
		wcCmd.Usage()
		return
	}

	files := wcCmd.Args()
	anySpecificCountFlag := *showLines || *showWords || *showBytes || *showChars || *showMaxLine
	if !anySpecificCountFlag { // Default behavior if no specific count flags are given
		*showLines = true
		*showWords = true
		*showBytes = true
	}

	var totalCounts wcCounts
	var processedFiles int
	exitCode := 0

	processOneInput := func(r io.Reader, filename string, isFile bool) {
		var currentCounts wcCounts
		var readerForProcessing io.Reader = r
		var sizeForBytes int64

		if isFile {
			file, ok := r.(*os.File)
			if !ok { // Should not happen if called correctly
				fmt.Fprintf(os.Stderr, "gilchrist wc: internal error processing file %s\n", filename)
				exitCode = 1
				return
			}
			stat, err := file.Stat()
			if err != nil {
				fmt.Fprintf(os.Stderr, "gilchrist wc: %s: %v\n", filename, err)
				exitCode = 1
				return
			}
			sizeForBytes = stat.Size()

		} else { // stdin
			content, err := io.ReadAll(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gilchrist wc: error reading stdin: %v\n", err)
				exitCode = 1
				return
			}
			sizeForBytes = int64(len(content))
			readerForProcessing = strings.NewReader(string(content))
		}

		if err := processStreamForWc(readerForProcessing, sizeForBytes, &currentCounts); err != nil {
			fmt.Fprintf(os.Stderr, "gilchrist wc: error processing %s: %v\n", filenameOrStdin(filename), err)
			exitCode = 1
			// Continue to print what was counted, if anything
		}

		printWcLine(currentCounts, *showLines, *showWords, *showBytes, *showChars, *showMaxLine, filenameOrStdin(filename))
		processedFiles++
		totalCounts.lines += currentCounts.lines
		totalCounts.words += currentCounts.words
		totalCounts.bytes += currentCounts.bytes
		totalCounts.chars += currentCounts.chars
		if currentCounts.maxLine > totalCounts.maxLine {
			totalCounts.maxLine = currentCounts.maxLine
		}
	}

	if len(files) == 0 || (len(files) == 1 && files[0] == "-") {
		processOneInput(os.Stdin, "", false)
	} else {
		for _, fpath := range files {
			file, err := os.Open(fpath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gilchrist wc: %s: %v\n", fpath, err)
				exitCode = 1
				continue
			}
			processOneInput(file, fpath, true)
			file.Close() // Close after processing this file
		}
	}

	if processedFiles > 1 {
		printWcLine(totalCounts, *showLines, *showWords, *showBytes, *showChars, *showMaxLine, "total")
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func filenameOrStdin(filename string) string {
	if filename == "" {
		return "" // For stdin, usually no name is printed unless it's explicitly from a '-' arg.
	}
	return filename
}

// --- FIND Implementation ---
// Helper to convert glob to regex
func globToRegex(glob string) string {
	var result strings.Builder
	result.WriteString("^")
	for _, r := range glob {
		switch r {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.', '+', '(', ')', '[', ']', '{', '}', '\\', '^', '$', '|':
			result.WriteString("\\")
			result.WriteRune(r)
		default:
			result.WriteRune(r)
		}
	}
	result.WriteString("$")
	return result.String()
}

func handleFind(args []string) {
	findCmd := flag.NewFlagSet("find", flag.ExitOnError)
	namePattern := findCmd.String("name", "", "Pattern for file name (glob, case-sensitive)")
	inamePattern := findCmd.String("iname", "", "Pattern for file name (glob, case-insensitive)")
	fileType := findCmd.String("type", "", "Type of file: 'f' (regular file) or 'd' (directory)")
	deleteFiles := findCmd.Bool("delete", false, "Delete found files/directories (USE WITH CAUTION!)")
	maxDepth := findCmd.Int("maxdepth", -1, "Maximum directory descent level (-1 for unlimited)")
	minDepth := findCmd.Int("mindepth", 0, "Minimum directory descent level (0 for starting points)")
	helpFlag := findCmd.Bool("h", false, "Display this help and exit")

	findCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gilchrist find [PATH...] [EXPRESSION...]\n")
		fmt.Fprintf(os.Stderr, "Search for files in a directory hierarchy. Default path is '.'.\n\n")
		fmt.Fprintf(os.Stderr, "Expressions:\n")
		findCmd.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  gilchrist find . -name \"*.txt\"\n")
		fmt.Fprintf(os.Stderr, "  gilchrist find /tmp -type d -iname \"cache*\"\n")
		fmt.Fprintf(os.Stderr, "  gilchrist find . -maxdepth 1 -name \"*.log\" -delete\n")
	}

	findCmd.Parse(args)

	if *helpFlag {
		findCmd.Usage()
		return
	}

	paths := findCmd.Args()
	if len(paths) == 0 {
		paths = []string{"."} // Default to current directory
	}

	var reName *regexp.Regexp
	var err error
	if *inamePattern != "" {
		reName, err = regexp.Compile("(?i)" + globToRegex(*inamePattern))
	} else if *namePattern != "" {
		reName, err = regexp.Compile(globToRegex(*namePattern))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gilchrist find: invalid pattern: %v\n", err)
		os.Exit(1)
	}

	exitCode := 0
	for _, p := range paths {
		absStartPath, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gilchrist find: error getting absolute path for %s: %v\n", p, err)
			exitCode = 1
			continue
		}

		err = filepath.WalkDir(absStartPath, func(currentPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// Check if the error is a permission denied on the starting path itself
				if currentPath == absStartPath && os.IsPermission(walkErr) {
					fmt.Fprintf(os.Stderr, "gilchrist find: %s: %v\n", currentPath, walkErr)
					return walkErr // Stop walking this path
				}
				// For other errors (e.g. permission denied on a subdirectory), print and skip
				fmt.Fprintf(os.Stderr, "gilchrist find: %s: %v (skipping)\n", currentPath, walkErr)
				if d != nil && d.IsDir() {
					return fs.SkipDir // Skip problematic directory
				}
				return nil // Continue with siblings
			}

			relPath, _ := filepath.Rel(absStartPath, currentPath)
			depth := 0
			if relPath != "." && relPath != "" {
				depth = len(strings.Split(filepath.ToSlash(relPath), "/"))
			}

			if *maxDepth != -1 && depth > *maxDepth {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			if depth < *minDepth {
				return nil // Don't process items shallower than minDepth
			}

			// Apply filters only after depth checks
			if *fileType != "" {
				isDir := d.IsDir()
				if (*fileType == "f" && isDir) || (*fileType == "d" && !isDir) {
					return nil
				}
			}

			if reName != nil {
				if !reName.MatchString(d.Name()) {
					return nil
				}
			}

			// Actions
			fmt.Println(currentPath) // Default action is to print

			if *deleteFiles {
				err := os.RemoveAll(currentPath) // Use RemoveAll for simplicity, handles files and dirs
				if err != nil {
					fmt.Fprintf(os.Stderr, "gilchrist find: failed to delete %s: %v\n", currentPath, err)
					exitCode = 1 // Mark that an error occurred
				} else {
					fmt.Fprintf(os.Stderr, "gilchrist find: deleted %s\n", currentPath)
				}
				if d.IsDir() && err == nil { // If a directory was successfully deleted, skip its further processing
					return fs.SkipDir
				}
			}
			return nil
		})
		if err != nil {
			// This error is from WalkDir itself, e.g., if the initial path doesn't exist.
			fmt.Fprintf(os.Stderr, "gilchrist find: error walking path %s: %v\n", p, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// --- RM Implementation ---
func handleRm(args []string) {
	rmCmd := flag.NewFlagSet("rm", flag.ExitOnError)
	recursive := rmCmd.Bool("r", false, "remove directories and their contents recursively")
	recursiveAlias := rmCmd.Bool("R", false, "alias for -r") // Both point to different vars initially, then combine
	force := rmCmd.Bool("f", false, "ignore nonexistent files and arguments, never prompt")
	interactive := rmCmd.Bool("i", false, "prompt before every removal")
	verbose := rmCmd.Bool("v", false, "explain what is being done")
	helpFlag := rmCmd.Bool("h", false, "display this help and exit")

	rmCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gilchrist rm [OPTION]... FILE...\n")
		fmt.Fprintf(os.Stderr, "Remove (unlink) the FILE(s).\n\n")
		fmt.Fprintf(os.Stderr, "WARNING: Use with caution, especially with -r and -f.\n")
		rmCmd.PrintDefaults()
	}

	rmCmd.Parse(args) // Supports combined boolean flags like -rf natively

	if *helpFlag {
		rmCmd.Usage()
		return
	}

	isRecursive := *recursive || *recursiveAlias // Combine -r and -R

	targets := rmCmd.Args()
	if len(targets) == 0 {
		if !*force { // GNU rm errors if no operands unless -f
			fmt.Fprintln(os.Stderr, "gilchrist rm: missing operand")
			rmCmd.Usage()
			os.Exit(1)
		}
		return // With -f and no args, do nothing silently
	}

	exitCode := 0
	inputReader := bufio.NewReader(os.Stdin)

	for _, target := range targets {
		info, err := os.Lstat(target) // Use Lstat to get info about symlink itself
		if err != nil {
			if os.IsNotExist(err) {
				if !*force {
					fmt.Fprintf(os.Stderr, "gilchrist rm: cannot remove '%s': No such file or directory\n", target)
					exitCode = 1
				}
				continue // Next target
			}
			// Other stat errors
			if !*force {
				fmt.Fprintf(os.Stderr, "gilchrist rm: cannot access '%s': %v\n", target, err)
				exitCode = 1
			}
			continue // Next target
		}

		isDir := info.IsDir()
		if isDir && !isRecursive {
			fmt.Fprintf(os.Stderr, "gilchrist rm: cannot remove '%s': Is a directory (not an error if -f)\n", target)
			if !*force { // Only an error if not forced
				exitCode = 1
			}
			continue
		}

		if *interactive && !*force { // -f overrides -i
			promptMsg := "remove"
			if isDir {
				promptMsg = "remove directory"
			}
			fmt.Printf("gilchrist rm: %s '%s'? ", promptMsg, target)
			response, _ := inputReader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(response)) != "y" {
				if *verbose {
					fmt.Printf("not removing '%s'\n", target)
				}
				continue
			}
		}

		var removeErr error
		if isDir && isRecursive {
			removeErr = os.RemoveAll(target)
		} else if !isDir { // It's a file or symlink
			removeErr = os.Remove(target)
		}

		if removeErr != nil {
			if !*force {
				fmt.Fprintf(os.Stderr, "gilchrist rm: failed to remove '%s': %v\n", target, removeErr)
				exitCode = 1
			}
		} else {
			if *verbose {
				fmt.Printf("removed '%s'\n", target)
			}
		}
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

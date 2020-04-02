package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/clipperhouse/jargon"
	"github.com/clipperhouse/jargon/ascii"
	"github.com/clipperhouse/jargon/contractions"
	"github.com/clipperhouse/jargon/stackoverflow"
	"github.com/clipperhouse/jargon/stemmer"
)

var filein = flag.String("file", "", "input file path (if none, stdin is used as input)")
var fileout = flag.String("out", "", "output file path (if none, stdout is used as input)")
var html = flag.Bool("html", false, "parse input as html (keep tags whole")
var lang = flag.String("lang", "english", "language of input, relevant when used with -stem. options:\n"+strings.Join(langs, ", "))

// These values aren't actually used, see filters loop below
var xascii = flag.Bool("ascii", false, "a filter to replace diacritics with ascii equivalents, e.g. café → cafe")
var xcontractions = flag.Bool("contractions", false, "a filter to expand contractions, e.g. Would've → Would have")
var xstack = flag.Bool("stack", false, "a filter to recognize tech terms as Stack Overflow tags, e.g. Ruby on Rails → ruby-on-rails")
var xstem = flag.Bool("stem", false, "a filter to stem words using snowball stemmer, e.g. management|manager → manag")

var filterMap = map[string]jargon.Filter{
	"-ascii":        ascii.Fold,
	"-contractions": contractions.Expander,
	"-stack":        stackoverflow.Tags,
	"-stem":         stemmer.English,
}

var langs = []string{"english", "french", "norwegian", "russian", "spanish", "swedish"}
var stemmerMap = map[string]jargon.Filter{
	"english":   stemmer.English,
	"french":    stemmer.French,
	"norwegian": stemmer.Norwegian,
	"russian":   stemmer.Russian,
	"spanish":   stemmer.Spanish,
	"swedish":   stemmer.Swedish,
}

func main() {
	flag.Parse()

	var c = config{
		HTML: *html,
	}

	//
	// Input
	//
	fi, err := os.Stdin.Stat()
	check(err)
	mode := fi.Mode()

	err = setInput(&c, mode, *filein)
	if err == errNoInput {
		// Display usage
		os.Stderr.WriteString(flag.CommandLine.Name() + " takes text from std input and processes it with one or more filters\n\n")
		os.Stderr.WriteString("Flags:\n")
		flag.PrintDefaults()
		return
	}
	check(err)
	if c.Filein != nil {
		defer c.Filein.Close()
	}

	//
	// Filters
	//
	err = setFilters(&c, os.Args[1:], *lang)
	check(err)

	//
	// Output
	//
	err = setOutput(&c)
	check(err)
	if c.Fileout != nil {
		defer c.Fileout.Close()
	}

	//
	// Reader
	//
	err = setReader(&c)
	check(err)

	//
	// Writer
	//
	err = setWriter(&c)
	check(err)

	//
	// Execute filters
	//
	err = execute(&c)
	check(err)
}

type config struct {
	HTML    bool
	Filters []jargon.Filter

	Filein, Fileout   *os.File
	Pipedin, Pipedout bool

	Reader *bufio.Reader
	Writer *bufio.Writer
}

var errNoInput = fmt.Errorf("no input")
var errTwoInput = fmt.Errorf("choose *either* an input -file argument *or* piped input")

func setInput(c *config, mode os.FileMode, filein string) error {
	if filein != "" {
		// Try to open it
		file, err := os.Open(filein)
		check(err)

		c.Filein = file
	}

	c.Pipedin = (mode & os.ModeCharDevice) == 0 // https://stackoverflow.com/a/43947435/70613

	// If no input, display usage
	input := c.Pipedin || c.Filein != nil
	if !input {
		return errNoInput
	}

	// Choose one input *or* the other
	if c.Pipedin && c.Filein != nil {
		return errTwoInput
	}

	return nil
}

func setFilters(c *config, args []string, lang string) error {
	// Loop through filters; order matters, so can't use flag package
	for _, arg := range args {
		filter, found := filterMap[arg]
		if found {
			if filter == stemmer.English {
				// Look for a language specification
				if lang != "" {
					stem, found := stemmerMap[lang]
					if found {
						filter = stem
					} else {
						err := fmt.Errorf("lang %q is not known by %s; options are %s", lang, flag.CommandLine.Name(), strings.Join(langs, ", "))
						return err
					}
				}
			}

			c.Filters = append(c.Filters, filter)
		}
	}

	return nil
}

func setOutput(c *config) error {
	if *fileout != "" {
		// Interpret last arg as output file path
		file, err := os.Create(*fileout)
		if err != nil {
			return err
		}

		c.Fileout = file
	}
	c.Pipedout = (c.Fileout == nil)

	return nil
}

func setReader(c *config) error {
	if c.Pipedin || c.Pipedout {
		// We're limited by the OS pipe buffer, typically 64K with back pressure
		// Using anything larger doesn't buy us anything
		size := 64 * 1024
		if c.Pipedin {
			c.Reader = bufio.NewReaderSize(os.Stdin, size)
		} else {
			c.Reader = bufio.NewReaderSize(c.Filein, size)
		}
	}

	if c.Filein != nil {
		fi, err := c.Filein.Stat()
		if err != nil {
			return err
		}

		size := fi.Size()
		switch {
		case size <= 4*1024:
			// Minimum of 4K
			c.Reader = bufio.NewReaderSize(c.Filein, 4*1024)
		case size <= 1024*1024:
			// Aim for a right-sized buffer (single read, perhaps) up to 1MB
			c.Reader = bufio.NewReaderSize(c.Filein, int(size))
		default:
			// Otherwise, use 1MB buffer size, better perf over default, but not huge
			c.Reader = bufio.NewReaderSize(c.Filein, 1024*1024)
		}
	}

	return nil
}

func setWriter(c *config) error {
	if c.Reader == nil {
		return fmt.Errorf("reader is required")
	}

	// Match the input buffer size; mismatch doesn't buy us anything
	size := c.Reader.Size()
	if c.Pipedout {
		c.Writer = bufio.NewWriterSize(os.Stdout, size)
	} else {
		c.Writer = bufio.NewWriterSize(c.Fileout, size)
	}

	return nil
}

func execute(c *config) error {
	if c.Reader == nil {
		return fmt.Errorf("reader is required")
	}
	if c.Writer == nil {
		return fmt.Errorf("writer is required")
	}

	var tokens *jargon.Tokens
	if c.HTML {
		tokens = jargon.TokenizeHTML(c.Reader)
	} else {
		tokens = jargon.Tokenize(c.Reader)
	}
	for _, f := range c.Filters {
		tokens = tokens.Filter(f)
	}

	if _, err := tokens.WriteTo(c.Writer); err != nil {
		return err
	}
	if err := c.Writer.Flush(); err != nil {
		return err
	}

	return nil
}

func check(err error) {
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Stderr.WriteString("\n")
		os.Exit(1)
	}
}

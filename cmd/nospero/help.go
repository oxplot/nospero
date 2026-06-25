package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

const (
	colorAuto   = "auto"
	colorAlways = "always"
	colorNever  = "never"
)

const appHelpTemplate = `{{section "NAME"}}
  {{name (helpName .)}}{{if .Usage}} {{muted "-"}} {{.Usage}}{{end}}

{{section "USAGE"}}
  {{cmd (helpName .)}}{{if .VisibleFlags}} {{arg "[global options]"}}{{end}}{{if commands .}} {{arg "command"}} {{arg "[command options]"}}{{end}}{{if .ArgsUsage}} {{arg .ArgsUsage}}{{end}}
{{$examples := examples .}}{{if $examples}}
{{section "EXAMPLES"}}
{{range $examples}}  {{example .}}
{{end}}{{end}}{{$commands := commands .}}{{if $commands}}
{{section "COMMANDS"}}{{range $commands}}
  {{commandNames .}}{{"\t"}}{{.Usage}}{{end}}{{end}}{{if .VisibleFlagCategories}}

{{section "GLOBAL OPTIONS"}}{{range .VisibleFlagCategories}}{{if .Name}}
  {{subsection .Name}}{{end}}{{range .Flags}}
  {{flag .}}{{end}}{{end}}{{else if .VisibleFlags}}

{{section "GLOBAL OPTIONS"}}{{range .VisibleFlags}}
  {{flag .}}{{end}}{{end}}{{if .Copyright}}

{{section "COPYRIGHT"}}
  {{.Copyright}}{{end}}
`

const commandHelpTemplate = `{{section "NAME"}}
  {{name (helpName .)}}{{if .Usage}} {{muted "-"}} {{.Usage}}{{end}}

{{section "USAGE"}}
  {{cmd (helpName .)}}{{if .VisibleFlags}} {{arg "[options]"}}{{end}}{{if .ArgsUsage}} {{arg .ArgsUsage}}{{else}}{{if .Args}} {{arg "[arguments...]"}}{{end}}{{end}}
{{$examples := examples .}}{{if $examples}}
{{section "EXAMPLES"}}
{{range $examples}}  {{example .}}
{{end}}{{end}}{{if .Description}}
{{section "DESCRIPTION"}}
  {{wrap .Description 2}}{{end}}{{if .VisibleFlagCategories}}
{{section "OPTIONS"}}{{range .VisibleFlagCategories}}{{if .Name}}
  {{subsection .Name}}{{end}}{{range .Flags}}
  {{flag .}}{{end}}{{end}}{{else if .VisibleFlags}}
{{section "OPTIONS"}}{{range .VisibleFlags}}
  {{flag .}}{{end}}{{end}}
`

const subcommandHelpTemplate = `{{section "NAME"}}
  {{name (helpName .)}}{{if .Usage}} {{muted "-"}} {{.Usage}}{{end}}

{{section "USAGE"}}
  {{cmd (helpName .)}}{{if .VisibleFlags}} {{arg "[options]"}}{{end}}{{if commands .}} {{arg "command"}} {{arg "[command options]"}}{{end}}{{if .ArgsUsage}} {{arg .ArgsUsage}}{{end}}
{{$examples := examples .}}{{if $examples}}
{{section "EXAMPLES"}}
{{range $examples}}  {{example .}}
{{end}}{{end}}{{$commands := commands .}}{{if $commands}}
{{section "COMMANDS"}}{{range $commands}}
  {{commandNames .}}{{"\t"}}{{.Usage}}{{end}}{{end}}{{if .VisibleFlagCategories}}
{{section "OPTIONS"}}{{range .VisibleFlagCategories}}{{if .Name}}
  {{subsection .Name}}{{end}}{{range .Flags}}
  {{flag .}}{{end}}{{end}}{{else if .VisibleFlags}}
{{section "OPTIONS"}}{{range .VisibleFlags}}
  {{flag .}}{{end}}{{end}}
`

func configureHelp(app *cli.App) {
	app.CustomAppHelpTemplate = appHelpTemplate
	cli.CommandHelpTemplate = commandHelpTemplate
	cli.SubcommandHelpTemplate = subcommandHelpTemplate
	cli.HelpPrinter = func(w io.Writer, templ string, data interface{}) {
		cli.HelpPrinterCustom(w, templ, data, helpTemplateFuncs(w, os.Args[1:]))
	}
}

func colorFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        "color",
			Usage:       "colorize help and diagnostics: auto, always, or never",
			Value:       colorAuto,
			DefaultText: colorAuto,
			EnvVars:     []string{"NOSPERO_COLOR"},
		},
		&cli.BoolFlag{
			Name:  "no-color",
			Usage: "disable color output; also respected through NO_COLOR",
		},
	}
}

func validateColorMode(c *cli.Context) error {
	mode := c.String("color")
	if c.Bool("no-color") {
		mode = colorNever
	}
	if !validColorMode(mode) {
		return fmt.Errorf("color must be one of auto, always, or never")
	}
	return nil
}

func helpTemplateFuncs(w io.Writer, args []string) map[string]interface{} {
	theme := newHelpTheme(colorEnabled(w, args))
	return map[string]interface{}{
		"arg":          theme.arg,
		"cmd":          theme.command,
		"commandNames": theme.commandNames,
		"commands":     visibleUserCommands,
		"example":      theme.example,
		"examples":     helpExamples,
		"flag":         theme.flag,
		"helpName":     helpName,
		"muted":        theme.muted,
		"name":         theme.name,
		"section":      theme.section,
		"subsection":   theme.subsection,
	}
}

func errorPrefix(w io.Writer) string {
	return newHelpTheme(colorEnabled(w, os.Args[1:])).error("error")
}

func colorEnabled(w io.Writer, args []string) bool {
	mode := colorModeFromArgs(args)
	if mode == colorNever {
		return false
	}
	if mode == colorAlways {
		return true
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
}

func colorModeFromArgs(args []string) string {
	mode := strings.TrimSpace(os.Getenv("NOSPERO_COLOR"))
	if mode == "" {
		mode = colorAuto
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-color":
			return colorNever
		case arg == "--color" && i+1 < len(args):
			mode = args[i+1]
			i++
		case strings.HasPrefix(arg, "--color="):
			mode = strings.TrimPrefix(arg, "--color=")
		}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !validColorMode(mode) {
		return colorAuto
	}
	return mode
}

func validColorMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case colorAuto, colorAlways, colorNever:
		return true
	default:
		return false
	}
}

type helpTheme struct {
	enabled bool
}

type rgbColor struct {
	r int
	g int
	b int
}

// xcodePalette uses Xcode Default (Dark) editor colors, mapped to CLI roles.
var xcodePalette = struct {
	selectionBlue      rgbColor
	foreground         rgbColor
	inactiveForeground rgbColor
	parameterBlue      rgbColor
	functionCyan       rgbColor
	typePurple         rgbColor
	errorRed           rgbColor
}{
	selectionBlue:      rgbColor{0x1f, 0x5f, 0xad},
	foreground:         rgbColor{0xb6, 0xb8, 0xbd},
	inactiveForeground: rgbColor{0x6f, 0x73, 0x77},
	parameterBlue:      rgbColor{0x2f, 0x8d, 0xad},
	functionCyan:       rgbColor{0x3f, 0xaf, 0xd0},
	typePurple:         rgbColor{0x9b, 0x7c, 0xc7},
	errorRed:           rgbColor{0xa8, 0x0f, 0x0e},
}

func newHelpTheme(enabled bool) helpTheme {
	return helpTheme{enabled: enabled}
}

func (t helpTheme) style(text string, rgb rgbColor, attrs ...color.Attribute) string {
	c := color.RGB(rgb.r, rgb.g, rgb.b).Add(attrs...)
	if t.enabled {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c.Sprint(text)
}

func (t helpTheme) section(text string) string {
	return t.style(strings.ToUpper(text), xcodePalette.selectionBlue, color.Bold)
}

func (t helpTheme) subsection(text string) string {
	return t.style(text, xcodePalette.typePurple, color.Bold)
}

func (t helpTheme) name(text string) string {
	return t.style(text, xcodePalette.foreground, color.Bold)
}

func (t helpTheme) command(text string) string {
	return t.style(text, xcodePalette.functionCyan, color.Bold)
}

func (t helpTheme) arg(text string) string {
	return t.style(text, xcodePalette.parameterBlue)
}

func (t helpTheme) muted(text string) string {
	return t.style(text, xcodePalette.inactiveForeground)
}

func (t helpTheme) error(text string) string {
	return t.style(text, xcodePalette.errorRed, color.Bold)
}

func (t helpTheme) example(text string) string {
	return t.muted("$ ") + t.command(text)
}

func (t helpTheme) commandNames(cmd *cli.Command) string {
	return t.command(strings.Join(cmd.Names(), ", "))
}

func (t helpTheme) flag(flag cli.Flag) string {
	name, usage, ok := strings.Cut(flag.String(), "\t")
	if !ok {
		return t.arg(flag.String())
	}
	return t.arg(name) + "\t" + usage
}

func helpName(data interface{}) string {
	switch v := data.(type) {
	case *cli.App:
		if v.HelpName != "" {
			return v.HelpName
		}
		return v.Name
	case *cli.Command:
		if v.HelpName != "" {
			return v.HelpName
		}
		return v.Name
	default:
		return ""
	}
}

func helpExamples(data interface{}) []string {
	name := helpName(data)
	if name == "" {
		return nil
	}
	switch name {
	case "nospero":
		return []string{
			`nospero status`,
			`nospero fonts add Roboto`,
			`nospero print text "Asset 42"`,
		}
	case "env", "nospero env":
		return []string{
			`nospero env`,
			`nospero --json env`,
		}
	case "fonts", "nospero fonts":
		return []string{
			`nospero fonts add Roboto`,
			`nospero fonts list`,
		}
	case "add", "nospero fonts add":
		return []string{
			`nospero fonts add Roboto`,
			`nospero fonts add https://fonts.google.com/specimen/Open+Sans`,
		}
	case "list", "nospero fonts list":
		return []string{
			`nospero fonts list`,
			`nospero --json fonts list`,
		}
	case "status", "nospero status":
		return []string{
			`nospero status`,
			`nospero --json --address 00:11:22:33:44:55 status`,
		}
	case "diagnose", "nospero diagnose":
		return []string{
			`nospero diagnose`,
			`nospero --json diagnose --probe reset-printer-then-reset-status`,
		}
	case "print", "nospero print":
		return []string{
			`nospero print text "Hello"`,
			`nospero print image --file label.png`,
			`nospero print mixed --file logo.png --text "Asset 42" --layout left`,
		}
	case "text", "nospero print text":
		return []string{
			`nospero print text "Hello"`,
			`nospero print text --font Roboto --font-weight 700 --italic "Hello"`,
			`nospero print text --text-align center --font Roboto "Top\nBottom"`,
		}
	case "image", "nospero print image":
		return []string{
			`nospero print image --file label.png`,
			`nospero print image --file label.png --preview-png preview.png --dry-run --tape-width-mm 24`,
		}
	case "mixed", "nospero print mixed":
		return []string{
			`nospero print mixed --file logo.png --text "Asset 42" --layout left`,
			`nospero print mixed --file logo.png --text "Asset 42" --font-weight 700 --layout above --gap-mm 1`,
		}
	default:
		return nil
	}
}

func visibleUserCommands(data interface{}) []*cli.Command {
	var commands []*cli.Command
	switch v := data.(type) {
	case *cli.App:
		commands = v.VisibleCommands()
	case *cli.Command:
		commands = v.VisibleCommands()
	default:
		return nil
	}
	out := make([]*cli.Command, 0, len(commands))
	for _, command := range commands {
		if command.Name == "help" {
			continue
		}
		out = append(out, command)
	}
	return out
}

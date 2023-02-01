package logger

import (
	"github.com/muesli/termenv"
)

var (
	p = termenv.ColorProfile()

	Red    termenv.Color = p.Color("#FF005F")
	Green  termenv.Color = p.Color("#00FF5F")
	Blue   termenv.Color = p.Color("#00afff")
	Yellow termenv.Color = p.Color("#FFC300")
)

package output

import "fmt"

const SuccessColorHex = "#B7C95C"

func SuccessMarker() string {
	return fmt.Sprintf("\x1b[38;2;183;201;92m%s\x1b[0m", SuccessMarkerText())
}

func SuccessMarkerText() string {
	return "✔︎"
}

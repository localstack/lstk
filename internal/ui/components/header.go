package components

import "github.com/localstack/lstk/internal/ui/styles"

func nimboLine1() string {
	return "    " +
		styles.NimboDark.Render("▟") +
		styles.NimboLight.Render("████▖") +
		"   "
}

func nimboLine2() string {
	return "   " +
		styles.NimboMid.Render("▟") +
		styles.NimboLight.Render("██▙█▙█") +
		styles.NimboMid.Render("▟") +
		"  "
}

func nimboLine3() string {
	return "     " +
		styles.NimboDark.Render("▀▛▀▛▀") +
		"   "
}

type Header struct {
	version string
}

func NewHeader(version string) Header {
	return Header{version: version}
}

func (h Header) View() string {
	title := styles.Title.Render("LocalStack (lstk)")
	version := styles.Version.Render(h.version)

	return "\n" + nimboLine1() + " " + title + "\n" +
		nimboLine2() + " " + version + "\n" +
		nimboLine3() + "\n"
}

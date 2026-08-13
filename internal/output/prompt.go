package output

import (
	"fmt"
	"strings"
)

// Keys carried by the options the constructors below build. Handlers should
// compare InputResponse.SelectedKey against these rather than string literals.
const (
	KeyYes = "y"
	KeyNo  = "n"
	// KeyAny matches any keypress. resolveOption in internal/ui returns it
	// before considering any other option, so it is only meaningful as the sole
	// option of an Acknowledge prompt.
	KeyAny = "any"
)

// ConfirmDefault selects which answer ENTER picks in a Confirm prompt. It is
// conveyed to the user by capitalizing that answer's label, which is also how
// the TUI's key resolution finds it — so the displayed default and the honored
// default cannot drift apart.
type ConfirmDefault int

const (
	DefaultYes ConfirmDefault = iota
	DefaultNo
)

// Confirm asks the user to approve an action they already requested, rendered
// inline as "Reset emulator state? [y/N]".
//
// Inline is deliberate here and should stay that way: the question has one
// answer the user is already leaning toward, the [y/N] idiom is universal, it
// costs one line, and it carries its default in the capitalization. Pass
// DefaultNo for anything destructive or irreversible.
//
// Use ActionChoice instead when the options are distinct outcomes rather than
// "do the thing I asked for, or don't". If a new prompt is not clearly one or
// the other, ask the user which it should be rather than guessing.
func Confirm(prompt string, def ConfirmDefault, responseCh chan<- InputResponse) UserInputRequestEvent {
	yes, no := "Y", "n"
	if def == DefaultNo {
		yes, no = "y", "N"
	}
	return UserInputRequestEvent{
		prompt: prompt,
		options: []InputOption{
			{Key: KeyYes, Label: yes},
			{Key: KeyNo, Label: no},
		},
		responseCh: responseCh,
	}
}

// ActionChoice offers a choice between distinct outcomes, rendered vertically
// as one selectable row per option:
//
//	? License validation failed: token expired.
//	● [ENTER] Log in again
//	○ [ESC] Exit
//
// Labels are plain prose — OptionLabel derives the bracketed shortcut from each
// option's Key, so a label must not spell the key out itself.
//
// Vertical is deliberate here and should stay that way: flattening several
// distinct actions into a trailing "[a/b]" hint reads as prose glued to the end
// of the question, wraps badly, and gives the user nothing to arrow through
// (DEVX-1045). Use Confirm for a yes/no on an action the user already
// requested, and Acknowledge when there is nothing to choose between.
func ActionChoice(prompt string, options []InputOption, responseCh chan<- InputResponse) UserInputRequestEvent {
	return UserInputRequestEvent{
		prompt:     prompt,
		options:    options,
		responseCh: responseCh,
		vertical:   true,
	}
}

// Acknowledge waits for any keypress, rendered inline as "Waiting for
// authorization... (Press any key when complete)". It is not a choice — there
// is one option and every key selects it — so it stays on one line.
func Acknowledge(prompt, label string, responseCh chan<- InputResponse) UserInputRequestEvent {
	return UserInputRequestEvent{
		prompt:     prompt,
		options:    []InputOption{{Key: KeyAny, Label: label}},
		responseCh: responseCh,
	}
}

// namedShortcuts spells out the keys whose names are not a single character.
// The values match what the terminal user is told to press, not tea.KeyType's
// own naming.
var namedShortcuts = map[string]string{
	"enter": "ENTER",
	"esc":   "ESC",
	"space": "SPACE",
	"tab":   "TAB",
}

// OptionLabel renders one option of a vertical prompt: its shortcut in
// brackets, then its label ("[ENTER] Log in again"). An option with no
// dedicated key — KeyAny, or an empty one — renders as the bare label, since
// there is no single key to advertise.
//
// Deriving the shortcut instead of leaving it to each label is what keeps the
// prompts consistent: hand-written labels had drifted into three styles
// ("[ENTER] Log in again", "Update now [U]", and a bare "AWS" that advertised
// no key at all) before this existed.
func OptionLabel(opt InputOption) string {
	key := shortcut(opt.Key)
	if key == "" {
		return opt.Label
	}
	if opt.Label == "" {
		return fmt.Sprintf("[%s]", key)
	}
	return fmt.Sprintf("[%s] %s", key, opt.Label)
}

// shortcut returns the display form of an option key, or "" when the key names
// no single keypress the user can be told to hit.
func shortcut(key string) string {
	if key == "" || key == KeyAny {
		return ""
	}
	if named, ok := namedShortcuts[key]; ok {
		return named
	}
	return strings.ToUpper(key)
}

// Package rdpclient embeds the FreeRDP protocol library with the lifecycle, dirty-region
// and input conventions the OnRDP UI needs.
package rdpclient

import (
	"slices"

	"github.com/onix-engine/onix/input"
)

// Key is one PC/AT set-1 scancode. Extended keys carry the E0 prefix, which the
// server needs to distinguish the navigation cluster from the numeric keypad.
type Key struct {
	Scancode uint16
	Extended bool
}

// Scancode maps an Onix key code to the scancode a Windows server expects.
// Unmapped keys report false so callers can fall back to Unicode input.
func Scancode(code input.KeyCode) (Key, bool) {
	key, ok := scancodes[code]
	return key, ok
}

// keycodes lists every mapped Onix key code once, sorted, so the per-frame input
// scan walks the mapping instead of the whole key-code space.
var keycodes = buildKeycodes()

func buildKeycodes() []input.KeyCode {
	codes := make([]input.KeyCode, 0, len(scancodes))
	for code := range scancodes {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return codes
}

// Keycodes returns the key codes that have a remote scancode. The slice is
// shared and must not be modified.
func Keycodes() []input.KeyCode { return keycodes }

var scancodes = map[input.KeyCode]Key{
	input.KeyEscape:       {Scancode: 0x01},
	input.KeyNum1:         {Scancode: 0x02},
	input.KeyNum2:         {Scancode: 0x03},
	input.KeyNum3:         {Scancode: 0x04},
	input.KeyNum4:         {Scancode: 0x05},
	input.KeyNum5:         {Scancode: 0x06},
	input.KeyNum6:         {Scancode: 0x07},
	input.KeyNum7:         {Scancode: 0x08},
	input.KeyNum8:         {Scancode: 0x09},
	input.KeyNum9:         {Scancode: 0x0A},
	input.KeyNum0:         {Scancode: 0x0B},
	input.KeyMinus:        {Scancode: 0x0C},
	input.KeyEqual:        {Scancode: 0x0D},
	input.KeyBackspace:    {Scancode: 0x0E},
	input.KeyTab:          {Scancode: 0x0F},
	input.KeyQ:            {Scancode: 0x10},
	input.KeyW:            {Scancode: 0x11},
	input.KeyE:            {Scancode: 0x12},
	input.KeyR:            {Scancode: 0x13},
	input.KeyT:            {Scancode: 0x14},
	input.KeyY:            {Scancode: 0x15},
	input.KeyU:            {Scancode: 0x16},
	input.KeyI:            {Scancode: 0x17},
	input.KeyO:            {Scancode: 0x18},
	input.KeyP:            {Scancode: 0x19},
	input.KeyLeftBracket:  {Scancode: 0x1A},
	input.KeyRightBracket: {Scancode: 0x1B},
	input.KeyEnter:        {Scancode: 0x1C},
	input.KeyLeftControl:  {Scancode: 0x1D},
	input.KeyA:            {Scancode: 0x1E},
	input.KeyS:            {Scancode: 0x1F},
	input.KeyD:            {Scancode: 0x20},
	input.KeyF:            {Scancode: 0x21},
	input.KeyG:            {Scancode: 0x22},
	input.KeyH:            {Scancode: 0x23},
	input.KeyJ:            {Scancode: 0x24},
	input.KeyK:            {Scancode: 0x25},
	input.KeyL:            {Scancode: 0x26},
	input.KeySemicolon:    {Scancode: 0x27},
	input.KeyApostrophe:   {Scancode: 0x28},
	input.KeyGraveAccent:  {Scancode: 0x29},
	input.KeyLeftShift:    {Scancode: 0x2A},
	input.KeyBackslash:    {Scancode: 0x2B},
	input.KeyZ:            {Scancode: 0x2C},
	input.KeyX:            {Scancode: 0x2D},
	input.KeyC:            {Scancode: 0x2E},
	input.KeyV:            {Scancode: 0x2F},
	input.KeyB:            {Scancode: 0x30},
	input.KeyN:            {Scancode: 0x31},
	input.KeyM:            {Scancode: 0x32},
	input.KeyComma:        {Scancode: 0x33},
	input.KeyPeriod:       {Scancode: 0x34},
	input.KeySlash:        {Scancode: 0x35},
	input.KeyRightShift:   {Scancode: 0x36},
	input.KeyKPMultiply:   {Scancode: 0x37},
	input.KeyLeftAlt:      {Scancode: 0x38},
	input.KeySpace:        {Scancode: 0x39},
	input.KeyCapsLock:     {Scancode: 0x3A},
	input.KeyF1:           {Scancode: 0x3B},
	input.KeyF2:           {Scancode: 0x3C},
	input.KeyF3:           {Scancode: 0x3D},
	input.KeyF4:           {Scancode: 0x3E},
	input.KeyF5:           {Scancode: 0x3F},
	input.KeyF6:           {Scancode: 0x40},
	input.KeyF7:           {Scancode: 0x41},
	input.KeyF8:           {Scancode: 0x42},
	input.KeyF9:           {Scancode: 0x43},
	input.KeyF10:          {Scancode: 0x44},
	input.KeyNumLock:      {Scancode: 0x45},
	input.KeyScrollLock:   {Scancode: 0x46},
	input.KeyKP7:          {Scancode: 0x47},
	input.KeyKP8:          {Scancode: 0x48},
	input.KeyKP9:          {Scancode: 0x49},
	input.KeyKPSubtract:   {Scancode: 0x4A},
	input.KeyKP4:          {Scancode: 0x4B},
	input.KeyKP5:          {Scancode: 0x4C},
	input.KeyKP6:          {Scancode: 0x4D},
	input.KeyKPAdd:        {Scancode: 0x4E},
	input.KeyKP1:          {Scancode: 0x4F},
	input.KeyKP2:          {Scancode: 0x50},
	input.KeyKP3:          {Scancode: 0x51},
	input.KeyKP0:          {Scancode: 0x52},
	input.KeyKPDecimal:    {Scancode: 0x53},
	input.KeyF11:          {Scancode: 0x57},
	input.KeyF12:          {Scancode: 0x58},
	input.KeyKPEnter:      {Scancode: 0x1C, Extended: true},
	input.KeyRightControl: {Scancode: 0x1D, Extended: true},
	input.KeyKPDivide:     {Scancode: 0x35, Extended: true},
	input.KeyPrintScreen:  {Scancode: 0x37, Extended: true},
	input.KeyRightAlt:     {Scancode: 0x38, Extended: true},
	input.KeyHome:         {Scancode: 0x47, Extended: true},
	input.KeyUp:           {Scancode: 0x48, Extended: true},
	input.KeyPageUp:       {Scancode: 0x49, Extended: true},
	input.KeyLeft:         {Scancode: 0x4B, Extended: true},
	input.KeyRight:        {Scancode: 0x4D, Extended: true},
	input.KeyEnd:          {Scancode: 0x4F, Extended: true},
	input.KeyDown:         {Scancode: 0x50, Extended: true},
	input.KeyPageDown:     {Scancode: 0x51, Extended: true},
	input.KeyInsert:       {Scancode: 0x52, Extended: true},
	input.KeyDelete:       {Scancode: 0x53, Extended: true},
	input.KeyLeftSuper:    {Scancode: 0x5B, Extended: true},
	input.KeyRightSuper:   {Scancode: 0x5C, Extended: true},
	input.KeyMenu:         {Scancode: 0x5D, Extended: true},
}

// Remote key sequences for the toolbar buttons. Each entry is a scancode press
// followed by its release, matching what a physical keyboard would send.
var (
	CtrlAltDelete = []Key{{Scancode: 0x1D}, {Scancode: 0x38}, {Scancode: 0x53, Extended: true}}
	CtrlAltEnd    = []Key{{Scancode: 0x1D}, {Scancode: 0x38}, {Scancode: 0x4F, Extended: true}}
	AltTab        = []Key{{Scancode: 0x38}, {Scancode: 0x0F}}
	AltShiftTab   = []Key{{Scancode: 0x38}, {Scancode: 0x2A}, {Scancode: 0x0F}}
	Win           = []Key{{Scancode: 0x5B, Extended: true}}
	Escape        = []Key{{Scancode: 0x01}}
)

//go:build js && wasm

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"syscall/js"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
	"github.com/mtrisic/vremeplov/core/loader"
	"github.com/mtrisic/vremeplov/core/monitor"
)

const (
	framePeriodMs = 1000.0 / core.FramesPerSecond
	// maxCatchUpFrames bounds how much emulated time one animation
	// callback may run when the tab lags (same policy as the TUI).
	maxCatchUpFrames = 5
)

func main() {
	m, err := newMachine()
	if err != nil {
		panic(err)
	}
	// Rewind history: a snapshot every second, one minute deep (~15 MB
	// of the tab's wasm memory).
	m.EnableHistory(50*core.TstatesPerFrame, 60)

	global := js.Global()
	doc := global.Get("document")
	canvas := doc.Call("getElementById", "screen")
	canvas.Set("width", viewW)
	canvas.Set("height", viewH)
	ctx := canvas.Call("getContext", "2d")
	imgData := ctx.Call("createImageData", viewW, viewH)
	jsPix := imgData.Get("data")
	status := doc.Call("getElementById", "status")
	setStatus := func(s string) { status.Set("textContent", s) }

	rgba := make([]byte, viewW*viewH*4)
	frame := make([]byte, core.FrameWidth*core.FrameHeight)
	render := func() {
		m.Frame(frame)
		frameRGBA(frame, rgba)
		js.CopyBytesToJS(jsPix, rgba)
		ctx.Call("putImageData", imgData, 0, 0)
	}

	// Keyboard: press the mapped matrix keys on keydown, release the
	// same set on keyup of the same physical key (event.code), so
	// layout-shifted characters release cleanly. The matrix holds keys
	// itself, so auto-repeat events are swallowed.
	held := map[string][]core.Key{}
	eventID := func(e js.Value) string {
		if c := e.Get("code"); c.Truthy() && c.String() != "" {
			return c.String()
		}
		return e.Get("key").String()
	}
	// Keys typed into a focused text field (the monitor REPL, the file
	// picker) belong to that field, not to the machine matrix.
	inputFocused := func() bool {
		a := doc.Get("activeElement")
		return a.Truthy() && a.Get("tagName").String() == "INPUT"
	}
	keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool() || e.Get("altKey").Bool() || inputFocused() {
			return nil
		}
		strokes, ok := keystrokesFor(e.Get("key").String())
		if !ok {
			return nil
		}
		e.Call("preventDefault")
		if e.Get("repeat").Bool() {
			return nil
		}
		held[eventID(e)] = strokes
		for _, k := range strokes {
			m.PressKey(k)
		}
		return nil
	})
	keyup := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		id := eventID(e) // release even if focus moved mid-hold
		for _, k := range held[id] {
			m.ReleaseKey(k)
		}
		delete(held, id)
		return nil
	})
	global.Call("addEventListener", "keydown", keydown)
	global.Call("addEventListener", "keyup", keyup)

	// Transport state shared by the controls below.
	var lastTape []byte // the picked .gtp, kept for "Reload tape"
	paused := false
	pauseBtn := doc.Call("getElementById", "pause")
	setPaused := func(p bool) {
		paused = p
		if p {
			pauseBtn.Set("textContent", "Resume")
		} else {
			pauseBtn.Set("textContent", "Pause")
		}
	}

	// dropAudio discards sound rendered across transport bursts (fast
	// boots, snapshot jumps) so they don't play as noise; assigned by
	// the sound block below.
	dropAudio := func() {}

	// .gtp picker: always start clean — reset, boot to READY, fast-load,
	// RUN. "Reload tape" repeats the same sequence for the current file.
	loadTape := func() {
		name, err := loader.LoadAndRun(m, lastTape)
		if err != nil {
			setStatus(fmt.Sprintf("load failed: %v", err))
			return
		}
		if name == "" {
			name = "program"
		}
		setPaused(false)
		dropAudio()
		render()
		setStatus(fmt.Sprintf("loaded %s — running", name))
	}
	fileInput := doc.Call("getElementById", "gtp")
	onLoaded := js.FuncOf(func(_ js.Value, args []js.Value) any {
		u8 := js.Global().Get("Uint8Array").New(args[0])
		lastTape = make([]byte, u8.Get("length").Int())
		js.CopyBytesToGo(lastTape, u8)
		loadTape()
		return nil
	})
	onFile := js.FuncOf(func(_ js.Value, args []js.Value) any {
		files := fileInput.Get("files")
		if files.Get("length").Int() == 0 {
			return nil
		}
		files.Index(0).Call("arrayBuffer").Call("then", onLoaded)
		fileInput.Call("blur") // give the keyboard back to the machine
		return nil
	})
	fileInput.Call("addEventListener", "change", onFile)

	reloadBtn := doc.Call("getElementById", "reload")
	onReload := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if lastTape == nil {
			setStatus("no tape loaded")
		} else {
			loadTape()
		}
		reloadBtn.Call("blur")
		return nil
	})
	reloadBtn.Call("addEventListener", "click", onReload)

	// Reset: fresh machine at the READY prompt, picked file forgotten.
	resetBtn := doc.Call("getElementById", "reset")
	onReset := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		loader.ResetToReady(m)
		lastTape = nil
		fileInput.Set("value", "")
		setPaused(false)
		dropAudio()
		render()
		setStatus("reset — READY")
		resetBtn.Call("blur")
		return nil
	})
	resetBtn.Call("addEventListener", "click", onReset)

	onPause := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		setPaused(!paused)
		if paused {
			setStatus("paused")
		} else {
			setStatus("running")
		}
		pauseBtn.Call("blur")
		return nil
	})
	pauseBtn.Call("addEventListener", "click", onPause)

	// Rewind: click steps 2 s back; holding the button keeps rewinding
	// (the time machine scrubs backwards even while running).
	// refreshMon is assigned by the monitor block below.
	var refreshMon func()
	rewBtn := doc.Call("getElementById", "rewind")
	doRewind := func() {
		if err := m.Rewind(100 * core.TstatesPerFrame); err != nil {
			setStatus("rewind: " + err.Error())
			return
		}
		render()
		refreshMon()
		setStatus(fmt.Sprintf("rewound to t=%.1fs", float64(m.Tstates())/core.CPUClockHz))
	}
	rewindTick := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		doRewind()
		return nil
	})
	var rewInterval js.Value
	startRew := js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Call("preventDefault")
		doRewind()
		rewInterval = global.Call("setInterval", rewindTick, 250)
		return nil
	})
	stopRew := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if rewInterval.Truthy() {
			global.Call("clearInterval", rewInterval)
			rewInterval = js.Value{}
		}
		rewBtn.Call("blur")
		return nil
	})
	rewBtn.Call("addEventListener", "mousedown", startRew)
	rewBtn.Call("addEventListener", "touchstart", startRew)
	for _, ev := range []string{"mouseup", "mouseleave", "touchend", "touchcancel"} {
		rewBtn.Call("addEventListener", ev, stopRew)
	}

	// Paste: clipboard text types into the machine through the same
	// deterministic keystroke queue as headless --type. TypeText
	// validates every rune (and ignores '\r') before queueing, so a
	// paste with untypeable characters changes nothing. A text field
	// with focus keeps its native paste.
	var pasteEnd uint64
	onPaste := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if inputFocused() {
			return nil
		}
		cd := args[0].Get("clipboardData")
		if !cd.Truthy() {
			return nil
		}
		text := cd.Call("getData", "text").String()
		if text == "" {
			return nil
		}
		args[0].Call("preventDefault")
		end, err := m.TypeText(text)
		if err != nil {
			setStatus("paste failed: " + err.Error())
			return nil
		}
		pasteEnd = end
		setPaused(false)
		setStatus(fmt.Sprintf("typing %d pasted characters…", len(text)))
		return nil
	})
	global.Call("addEventListener", "paste", onPaste)

	// Tape recording: arm the core recorder, and on stop wrap the
	// captured SAVEs into a .gtp the browser downloads.
	recBtn := doc.Call("getElementById", "record")
	recFmt := doc.Call("getElementById", "recfmt")
	downloadBytes := func(name, mime string, data []byte) {
		u8 := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(u8, data)
		blob := js.Global().Get("Blob").New(
			[]any{u8}, map[string]any{"type": mime})
		url := js.Global().Get("URL").Call("createObjectURL", blob)
		a := doc.Call("createElement", "a")
		a.Set("href", url)
		a.Set("download", name)
		a.Call("click")
		js.Global().Get("URL").Call("revokeObjectURL", url)
	}
	onRecord := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		defer recBtn.Call("blur")
		if !m.TapeRecording() {
			m.StartTapeRecording()
			recBtn.Set("textContent", "Stop rec")
			setStatus("recording tape — SAVE in BASIC, then stop")
			return nil
		}
		recBtn.Set("textContent", "Record")
		streams := m.StopTapeRecording()
		if len(streams) == 0 {
			setStatus("recording stopped — no tape output captured")
			return nil
		}
		var (
			name string
			mime string
			out  []byte
		)
		if recFmt.Get("value").String() == "wav" {
			name = fmt.Sprintf("recording-%d.wav", m.FrameSeq())
			mime = "audio/wav"
			out = core.CompileTapeBlocks(streams...).EncodeWAV(44100)
		} else {
			img, err := gtp.Build("recording", streams...)
			if err != nil {
				setStatus("recording failed: " + err.Error())
				return nil
			}
			name = fmt.Sprintf("recording-%d.gtp", m.FrameSeq())
			mime = "application/octet-stream"
			out = img
		}
		downloadBytes(name, mime, out)
		setStatus(fmt.Sprintf("%d tape block(s) saved as %s", len(streams), name))
		return nil
	})
	recBtn.Call("addEventListener", "click", onRecord)

	// Sound: the cassette-port speaker trick — games click and beep
	// through the tape-out DAC, and core renders that as a sample
	// stream (RenderAudio). Here it feeds Web Audio as scheduled
	// buffers; the AudioContext is created on the button click, since
	// browsers require a user gesture to start audio.
	sndBtn := doc.Call("getElementById", "sound")
	var audioCtx js.Value
	var playhead float64
	pumpAudio := func() {
		if !m.AudioEnabled() || !audioCtx.Truthy() {
			return
		}
		rate := int(audioCtx.Get("sampleRate").Float())
		samples := m.RenderAudio(rate)
		if len(samples) == 0 {
			return
		}
		raw := make([]byte, 4*len(samples))
		for i, v := range samples {
			binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(v))
		}
		u8 := js.Global().Get("Uint8Array").New(len(raw))
		js.CopyBytesToJS(u8, raw)
		f32 := js.Global().Get("Float32Array").New(u8.Get("buffer"), 0, len(samples))
		buf := audioCtx.Call("createBuffer", 1, len(samples), rate)
		buf.Call("getChannelData", 0).Call("set", f32)
		src := audioCtx.Call("createBufferSource")
		src.Set("buffer", buf)
		src.Call("connect", audioCtx.Get("destination"))
		now := audioCtx.Get("currentTime").Float()
		if playhead < now+0.02 {
			playhead = now + 0.05 // (re)prime the scheduling lead
		}
		src.Call("start", playhead)
		playhead += float64(len(samples)) / float64(rate)
	}
	dropAudio = func() {
		if m.AudioEnabled() && audioCtx.Truthy() {
			m.RenderAudio(int(audioCtx.Get("sampleRate").Float()))
		}
	}
	onSound := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		defer sndBtn.Call("blur")
		if m.AudioEnabled() {
			m.DisableAudio()
			if audioCtx.Truthy() {
				audioCtx.Call("suspend")
			}
			sndBtn.Set("textContent", "Sound")
			setStatus("sound off")
			return nil
		}
		if !audioCtx.Truthy() {
			ac := global.Get("AudioContext")
			if !ac.Truthy() {
				ac = global.Get("webkitAudioContext")
			}
			if !ac.Truthy() {
				setStatus("sound: this browser has no Web Audio")
				return nil
			}
			audioCtx = ac.New()
		}
		audioCtx.Call("resume")
		m.EnableAudio()
		playhead = 0
		sndBtn.Set("textContent", "Mute")
		setStatus("sound on — the cassette-port speaker")
		return nil
	})
	sndBtn.Call("addEventListener", "click", onSound)

	// Screenshot: the active-area frame as a grayscale PNG — core's
	// deterministic encoder, so it matches headless --dump-frame --crop
	// byte for byte.
	shotBtn := doc.Call("getElementById", "shot")
	onShot := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		defer shotBtn.Call("blur")
		data, err := m.FramePNG(true)
		if err != nil {
			setStatus("screenshot: " + err.Error())
			return nil
		}
		name := fmt.Sprintf("vremeplov-shot-%d.png", m.FrameSeq())
		downloadBytes(name, "image/png", data)
		setStatus("screenshot saved as " + name)
		return nil
	})
	shotBtn.Call("addEventListener", "click", onShot)

	// Save states: Save downloads the machine as a gob snapshot file;
	// Load restores one exactly (tape deck included) — the same format
	// cmd/headless and the TUI use, so states move between frontends.
	snapSaveBtn := doc.Call("getElementById", "snapsave")
	onSnapSave := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		defer snapSaveBtn.Call("blur")
		data, err := snapshotBytes(m)
		if err != nil {
			setStatus("snapshot: " + err.Error())
			return nil
		}
		name := fmt.Sprintf("vremeplov-snap-%d.gob", m.FrameSeq())
		downloadBytes(name, "application/octet-stream", data)
		setStatus("snapshot saved as " + name)
		return nil
	})
	snapSaveBtn.Call("addEventListener", "click", onSnapSave)

	snapLoadBtn := doc.Call("getElementById", "snapload")
	snapFile := doc.Call("getElementById", "snapfile")
	onSnapLoaded := js.FuncOf(func(_ js.Value, args []js.Value) any {
		u8 := js.Global().Get("Uint8Array").New(args[0])
		data := make([]byte, u8.Get("length").Int())
		js.CopyBytesToGo(data, u8)
		if err := restoreSnapshot(m, data); err != nil {
			setStatus("snapshot: " + err.Error())
			return nil
		}
		dropAudio()
		render()
		refreshMon()
		setStatus(fmt.Sprintf("snapshot restored (t=%.1fs)", float64(m.Tstates())/core.CPUClockHz))
		return nil
	})
	onSnapFile := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		files := snapFile.Get("files")
		if files.Get("length").Int() == 0 {
			return nil
		}
		files.Index(0).Call("arrayBuffer").Call("then", onSnapLoaded)
		snapFile.Set("value", "") // allow re-picking the same file
		return nil
	})
	snapFile.Call("addEventListener", "change", onSnapFile)
	onSnapLoad := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		snapFile.Call("click")
		snapLoadBtn.Call("blur")
		return nil
	})
	snapLoadBtn.Call("addEventListener", "click", onSnapLoad)

	// Monitor: the shared debugger engine (core/monitor) behind a
	// hidden-by-default panel. Opening pauses the machine; a breakpoint
	// or watchpoint hit pauses and opens it. The REPL input owns the
	// keyboard while focused; click the page (or Tab away) to type into
	// the Galaksija with the panel open.
	sess := monitor.New(m)
	monBtn := doc.Call("getElementById", "monitor")
	monPanel := doc.Call("getElementById", "monpanel")
	monState := doc.Call("getElementById", "monstate")
	monLogEl := doc.Call("getElementById", "monlog")
	monCmd := doc.Call("getElementById", "moncmd")
	var monLog []string
	appendLog := func(lines ...string) {
		monLog = append(monLog, lines...)
		if len(monLog) > 200 {
			monLog = monLog[len(monLog)-200:]
		}
	}
	monOpen := func() bool { return !monPanel.Get("hidden").Bool() }
	refreshMon = func() {
		if !monOpen() {
			return
		}
		var b strings.Builder
		for _, l := range sess.RegisterLines() {
			b.WriteString(l + "\n")
		}
		b.WriteString("── disasm ──────────────────────────\n")
		for _, l := range sess.DisasmLines(8) {
			b.WriteString(l + "\n")
		}
		if w := sess.WatchLines(); len(w) > 0 {
			b.WriteString("── watches ─────────────────────────\n")
			b.WriteString(strings.Join(w, "\n") + "\n")
		}
		monState.Set("textContent", strings.TrimRight(b.String(), "\n"))
		monLogEl.Set("textContent", strings.Join(monLog, "\n"))
		monLogEl.Set("scrollTop", monLogEl.Get("scrollHeight"))
	}
	openMonitor := func() {
		monPanel.Set("hidden", false)
		setPaused(true)
		sess.Paused = true
		refreshMon()
	}
	onMonBtn := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if monOpen() {
			monPanel.Set("hidden", true)
			setStatus("monitor closed")
		} else {
			openMonitor()
			setStatus("monitor — paused; type help at its prompt")
			monCmd.Call("focus")
		}
		monBtn.Call("blur")
		return nil
	})
	monBtn.Call("addEventListener", "click", onMonBtn)
	onCmdKey := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if e.Get("key").String() != "Enter" {
			return nil
		}
		line := strings.TrimSpace(monCmd.Get("value").String())
		monCmd.Set("value", "")
		if line == "" {
			return nil
		}
		appendLog("> " + line)
		sess.Paused = paused
		appendLog(sess.Exec(line)...)
		setPaused(sess.Paused)
		render()
		refreshMon()
		return nil
	})
	monCmd.Call("addEventListener", "keydown", onCmdKey)

	// 50 Hz machine on a 60 Hz animation clock: run whole frames for
	// the elapsed wall time, capped so a suspended tab doesn't spiral.
	// Frames run through RunDebug so breakpoints and watchpoints stop
	// the machine even with the panel closed.
	var tick js.Func
	last := -1.0
	tick = js.FuncOf(func(_ js.Value, args []js.Value) any {
		now := args[0].Float()
		if last < 0 || paused {
			last = now
		}
		frames := int((now - last) / framePeriodMs)
		if frames >= maxCatchUpFrames {
			frames = maxCatchUpFrames
			last = now
		} else {
			last += float64(frames) * framePeriodMs
		}
		for i := 0; i < frames; i++ {
			boundary := m.Tstates() - m.Tstates()%core.TstatesPerFrame + core.TstatesPerFrame
			if s := m.RunDebug(boundary - m.Tstates()); s.Reason != core.StopBudget {
				msg := monitor.FormatStop(s)
				appendLog(msg)
				openMonitor()
				appendLog(sess.PCLine())
				setStatus(msg)
				break
			}
		}
		if frames > 0 {
			pumpAudio()
			render()
			refreshMon()
			if pasteEnd != 0 && m.Tstates() >= pasteEnd {
				pasteEnd = 0
				setStatus("pasted text typed")
			}
		}
		global.Call("requestAnimationFrame", tick)
		return nil
	})
	setStatus("READY — type here; pick a .gtp to load")
	global.Call("requestAnimationFrame", tick)
	select {}
}

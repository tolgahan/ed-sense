# HUD reader tools

The Python scripts here (`ref2.py`, `ref3.py` and their `*frame.py` helpers) were used to develop the HUD reader (`internal/hud`) and to build its 12x20 glyph templates (`internal/hud/glyphs.go`):

1. frames were extracted from a recorded fight (2560x1440, 105 s) with ffmpeg;
2. the shield and heat readings were labelled by hand;
3. the templates are the mean of the labelled, straightened glyph cells.

The shield templates come from `ref3.py`, the heat templates from `ref2.py`. The Go code has moved on since (hue-based colour matching, the colour check, the 100% rule for four-glyph shield readings), so the Go tests check against the hand labels. They read recorded gameplay that is not in the repository, from these folders:

- `HUD_FRAMES`: the recording's frames with `labels_shield.json` / `labels_heat.json` (file -> value) and `golden_calib.json`. `TestRecordingAccuracy` reads them as recorded, `TestColourMatrixFrames` recoloured with several colour matrices, `TestCalibrate` and the `TestWatcher...Colours` tests look for the colours on screen.
- `HUD_CROPS`: captures made by the reader itself (`hud_debug` crops from a 4K game window) with `truth_shield.json` / `truth_heat.json`, for `Test4KCaptureAccuracy`.
- `HUD_WPN`: fire group list captures (`*_weaponsL-*.png`, `*_weaponsR-*.png`, 4K) for `TestListsReal` and `TestWatcherLists`; the lists are also read scaled to 1440p and 1080p.
- `HUD_SEQ`: a 10 fps frame sequence for `TestWatcherSequence` (tracking and hit detection).

Accuracy per frame: on the 4K captures 99% shield, 90% heat; on the compressed recording 90% shield, 82% heat. Most heat misses are radar contacts covering a digit; the filter in `internal/hud/filter.go` turns single reads into a steady value.

The fire group lists have no glyph templates. Entries are found as text lines with a thin segmented bar under them, and matched against the ship's modules by width in glyph heights (letter widths measured on 4K captures), the mount icon and the ammo line. On 88 captures of a Krait Mk II (3 multi-cannons and 2 heat sinks on L2, 2 beam lasers on R2) at 4K, the multi-cannons are found in 77 of 82 left captures (the others out of range or out of view), the heat sinks in 82, the beams in 6 of 6, and the 3 RELOADING captures with no false reloads; about the same at 1440p and 1080p.

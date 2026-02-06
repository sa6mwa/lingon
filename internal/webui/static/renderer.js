const ANSI_CLEAR = "\x1b[2J";
const ANSI_HOME = "\x1b[H";
const ANSI_HIDE_CURSOR = "\x1b[?25l";
const ANSI_SHOW_CURSOR = "\x1b[?25h";
const ANSI_RESET = "\x1b[0m";
const ANSI_CLEAR_LINE = "\x1b[K";

const MODE_BOLD = 1 << 0;
const MODE_FAINT = 1 << 1;
const MODE_ITALIC = 1 << 2;
const MODE_UNDERLINE = 1 << 3;
const MODE_BLINK = 1 << 4;
const MODE_INVERSE = 1 << 5;
const MODE_HIDDEN = 1 << 6;

const COLOR_DEFAULT = 0;
const COLOR_INDEXED = 1 << 24;
const COLOR_INDEXED_256 = 3 << 24;
const COLOR_TRUE = 2 << 24;
const COLOR_FLAG_MASK = 0xff000000;
const COLOR_VALUE_MASK = 0x00ffffff;

let widthProvider = null;

export function setGraphemeWidthProvider(fn) {
  widthProvider = typeof fn === "function" ? fn : null;
}

function cellWidth(grapheme, rune) {
  const text = grapheme || String.fromCodePoint(rune || 32);
  if (widthProvider) {
    const width = widthProvider(text);
    return width > 0 ? width : 1;
  }
  return grapheme ? 2 : 1;
}

function sgr(attr) {
  const codes = ["0"];
  if (attr.mode & MODE_BOLD) codes.push("1");
  if (attr.mode & MODE_FAINT) codes.push("2");
  if (attr.mode & MODE_ITALIC) codes.push("3");
  if (attr.mode & MODE_UNDERLINE) codes.push("4");
  if (attr.mode & MODE_BLINK) codes.push("5");
  if (attr.mode & MODE_INVERSE) codes.push("7");
  if (attr.mode & MODE_HIDDEN) codes.push("8");
  codes.push(...colorCode(true, attr.fg));
  codes.push(...colorCode(false, attr.bg));
  return `\x1b[${codes.join(";")}m`;
}

function colorCode(fg, val) {
  if (val === COLOR_DEFAULT) {
    return [fg ? "39" : "49"];
  }
  const flag = val & COLOR_FLAG_MASK;
  const raw = val & COLOR_VALUE_MASK;
  if (flag === COLOR_INDEXED) {
    if (raw < 16) {
      if (fg) {
        return [raw < 8 ? `${30 + raw}` : `${90 + (raw - 8)}`];
      }
      return [raw < 8 ? `${40 + raw}` : `${100 + (raw - 8)}`];
    }
    return [fg ? "38" : "48", "5", `${raw}`];
  }
  if (flag === COLOR_INDEXED_256) {
    return [fg ? "38" : "48", "5", `${raw}`];
  }
  if (flag === COLOR_TRUE) {
    const r = (raw >> 16) & 0xff;
    const g = (raw >> 8) & 0xff;
    const b = raw & 0xff;
    return [fg ? "38" : "48", "2", `${r}`, `${g}`, `${b}`];
  }
  return [fg ? "39" : "49"];
}

function attrEqual(a, b) {
  return a.mode === b.mode && a.fg === b.fg && a.bg === b.bg;
}

function isContinuationCell(snapshot, idx, attr) {
  if (!snapshot || !snapshot.runes || idx < 0 || idx >= snapshot.runes.length) {
    return false;
  }
  if (snapshot.graphemes && snapshot.graphemes[idx]) {
    return false;
  }
  const r = snapshot.runes[idx] || 0;
  if (r !== 0) {
    return false;
  }
  const mode = snapshot.modes[idx] || 0;
  const fg = snapshot.fg[idx] || 0;
  const bg = snapshot.bg[idx] || 0;
  return mode === attr.mode && fg === attr.fg && bg === attr.bg;
}

export function applyDiffToSnapshot(snapshot, diff) {
  if (!diff) return snapshot;
  let cols = diff.cols || (snapshot ? snapshot.cols : 0);
  let rows = diff.rows || (snapshot ? snapshot.rows : 0);
  let hasGraphemes = false;
  for (const row of diff.diffRows || []) {
    if (row.graphemes && row.graphemes.length > 0) {
      hasGraphemes = true;
      break;
    }
  }
  if (!snapshot || snapshot.cols !== cols || snapshot.rows !== rows) {
    snapshot = {
      cols,
      rows,
      runes: new Array(cols * rows).fill(0),
      modes: new Array(cols * rows).fill(0),
      fg: new Array(cols * rows).fill(0),
      bg: new Array(cols * rows).fill(0),
      graphemes: hasGraphemes ? new Array(cols * rows).fill("") : [],
      cursor: null,
      cursorVisible: false,
      mode: 0,
      title: "",
    };
  }
  if (snapshot.graphemes && snapshot.graphemes.length === 0 && hasGraphemes) {
    snapshot.graphemes = new Array(cols * rows).fill("");
  }

  for (const row of diff.diffRows || []) {
    const y = row.row;
    if (y < 0 || y >= rows) continue;
    const start = y * cols;
    for (let x = 0; x < cols; x++) {
      const idx = start + x;
      if (x < row.runes.length) snapshot.runes[idx] = row.runes[x];
      if (x < row.modes.length) snapshot.modes[idx] = row.modes[x];
      if (x < row.fg.length) snapshot.fg[idx] = row.fg[x];
      if (x < row.bg.length) snapshot.bg[idx] = row.bg[x];
      if (snapshot.graphemes && snapshot.graphemes.length > 0 && row.graphemes && x < row.graphemes.length) {
        snapshot.graphemes[idx] = row.graphemes[x];
      }
    }
  }
  if (diff.cursor) snapshot.cursor = diff.cursor;
  snapshot.cursorVisible = diff.cursorVisible;
  snapshot.mode = diff.mode;
  snapshot.title = diff.title;
  return snapshot;
}

export function renderSnapshot(snapshot, clear) {
  if (!snapshot) return "";
  const cols = snapshot.cols;
  const rows = snapshot.rows;
  const output = [];

  if (clear) {
    output.push(ANSI_CLEAR, ANSI_HOME);
  }

  output.push(snapshot.cursorVisible ? ANSI_SHOW_CURSOR : ANSI_HIDE_CURSOR);
  output.push(ANSI_RESET);

  const defaultAttr = { mode: 0, fg: COLOR_DEFAULT, bg: COLOR_DEFAULT };
  let current = defaultAttr;

  for (let y = 0; y < rows; y++) {
    output.push(`\x1b[${y + 1};1H`, sgr(defaultAttr));
    current = defaultAttr;
    for (let x = 0; x < cols; x++) {
      const idx = y * cols + x;
      let r = snapshot.runes[idx] || 32;
      let g = "";
      if (snapshot.graphemes && snapshot.graphemes.length > 0) {
        g = snapshot.graphemes[idx] || "";
      }
      const attr = {
        mode: snapshot.modes[idx] || 0,
        fg: snapshot.fg[idx] || 0,
        bg: snapshot.bg[idx] || 0,
      };
      const width = cellWidth(g, r);
      const skipNext = width > 1 && x + 1 < cols && isContinuationCell(snapshot, idx + 1, attr);
      if (attr.mode & MODE_HIDDEN) {
        g = "";
        r = 32;
      }
      if (!attrEqual(current, attr)) {
        output.push(sgr(attr));
        current = attr;
      }
      if (g) {
        output.push(g);
      } else {
        output.push(String.fromCodePoint(r));
      }
      if (skipNext) {
        x++;
      }
    }
    output.push(ANSI_CLEAR_LINE);
  }

  if (snapshot.cursor && snapshot.cursorVisible) {
    const row = Math.min(Math.max(snapshot.cursor.y, 0), rows - 1);
    const col = Math.min(Math.max(snapshot.cursor.x, 0), cols - 1);
    output.push(`\x1b[${row + 1};${col + 1}H`);
  }

  return output.join("");
}

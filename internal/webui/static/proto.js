const WIRE_VARINT = 0;
const WIRE_BYTES = 2;

function readVarint(bytes, offset) {
  let result = 0;
  let shift = 0;
  while (offset < bytes.length) {
    const b = bytes[offset++];
    result |= (b & 0x7f) << shift;
    if ((b & 0x80) === 0) {
      return { value: result >>> 0, offset };
    }
    shift += 7;
  }
  throw new Error("invalid varint");
}

function readBytes(bytes, offset) {
  const lenInfo = readVarint(bytes, offset);
  const len = lenInfo.value;
  const start = lenInfo.offset;
  const end = start + len;
  if (end > bytes.length) {
    throw new Error("invalid length");
  }
  return { value: bytes.slice(start, end), offset: end };
}

function readString(bytes, offset) {
  const out = readBytes(bytes, offset);
  return { value: new TextDecoder().decode(out.value), offset: out.offset };
}

function decodePackedVarints(bytes) {
  const out = [];
  let offset = 0;
  while (offset < bytes.length) {
    const res = readVarint(bytes, offset);
    out.push(res.value >>> 0);
    offset = res.offset;
  }
  return out;
}

function decodePackedInt32(bytes) {
  const out = [];
  let offset = 0;
  while (offset < bytes.length) {
    const res = readVarint(bytes, offset);
    let val = res.value | 0;
    if (res.value > 0x7fffffff) {
      val = res.value - 0x100000000;
    }
    out.push(val);
    offset = res.offset;
  }
  return out;
}

function decodeCursor(bytes) {
  const cursor = { x: 0, y: 0 };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      cursor.x = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 2 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      cursor.y = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported cursor wire");
  }
  return cursor;
}

function decodeSnapshot(bytes) {
  const snap = {
    cols: 0,
    rows: 0,
    runes: [],
    modes: [],
    fg: [],
    bg: [],
    graphemes: [],
    cursor: null,
    cursorVisible: false,
    mode: 0,
    title: "",
  };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readVarint(bytes, offset);
        snap.cols = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        snap.rows = res.value;
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readBytes(bytes, offset);
        snap.runes = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readBytes(bytes, offset);
        snap.modes = decodePackedInt32(res.value);
        offset = res.offset;
        break;
      }
      case 5: {
        const res = readBytes(bytes, offset);
        snap.fg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 6: {
        const res = readBytes(bytes, offset);
        snap.bg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 7: {
        const res = readBytes(bytes, offset);
        snap.cursor = decodeCursor(res.value);
        offset = res.offset;
        break;
      }
      case 8: {
        const res = readVarint(bytes, offset);
        snap.cursorVisible = res.value !== 0;
        offset = res.offset;
        break;
      }
      case 9: {
        const res = readVarint(bytes, offset);
        snap.mode = res.value;
        offset = res.offset;
        break;
      }
      case 10: {
        const res = readString(bytes, offset);
        snap.title = res.value;
        offset = res.offset;
        break;
      }
      case 11: {
        const res = readString(bytes, offset);
        snap.graphemes.push(res.value);
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported snapshot wire");
      }
    }
  }
  return snap;
}

function decodeDiffRow(bytes) {
  const row = { row: 0, runes: [], modes: [], fg: [], bg: [], graphemes: [] };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readVarint(bytes, offset);
        row.row = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readBytes(bytes, offset);
        row.runes = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readBytes(bytes, offset);
        row.modes = decodePackedInt32(res.value);
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readBytes(bytes, offset);
        row.fg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 5: {
        const res = readBytes(bytes, offset);
        row.bg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 6: {
        const res = readString(bytes, offset);
        row.graphemes.push(res.value);
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported diffrow wire");
      }
    }
  }
  return row;
}

function decodeDiff(bytes) {
  const diff = {
    cols: 0,
    rows: 0,
    diffRows: [],
    cursor: null,
    cursorVisible: false,
    mode: 0,
    title: "",
  };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readVarint(bytes, offset);
        diff.cols = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        diff.rows = res.value;
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readBytes(bytes, offset);
        diff.diffRows.push(decodeDiffRow(res.value));
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readBytes(bytes, offset);
        diff.cursor = decodeCursor(res.value);
        offset = res.offset;
        break;
      }
      case 5: {
        const res = readVarint(bytes, offset);
        diff.cursorVisible = res.value !== 0;
        offset = res.offset;
        break;
      }
      case 6: {
        const res = readVarint(bytes, offset);
        diff.mode = res.value;
        offset = res.offset;
        break;
      }
      case 7: {
        const res = readString(bytes, offset);
        diff.title = res.value;
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported diff wire");
      }
    }
  }
  return diff;
}

function decodeScrollbackRow(bytes) {
  const row = { runes: [], modes: [], fg: [], bg: [], graphemes: [] };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readBytes(bytes, offset);
        row.runes = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readBytes(bytes, offset);
        row.modes = decodePackedInt32(res.value);
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readBytes(bytes, offset);
        row.fg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readBytes(bytes, offset);
        row.bg = decodePackedVarints(res.value);
        offset = res.offset;
        break;
      }
      case 5: {
        const res = readString(bytes, offset);
        row.graphemes.push(res.value);
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported scrollback row wire");
      }
    }
  }
  return row;
}

function decodeScrollback(bytes) {
  const scrollback = { cols: 0, clear: false, rows: [] };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readVarint(bytes, offset);
        scrollback.cols = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        scrollback.clear = res.value !== 0;
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readBytes(bytes, offset);
        scrollback.rows.push(decodeScrollbackRow(res.value));
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported scrollback wire");
      }
    }
  }
  return scrollback;
}

function decodeHello(bytes) {
  const hello = {
    clientId: "",
    cols: 0,
    rows: 0,
    wantsControl: false,
    lastSeq: 0,
    clientType: "",
  };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readString(bytes, offset);
        hello.clientId = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        hello.cols = res.value;
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readVarint(bytes, offset);
        hello.rows = res.value;
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readVarint(bytes, offset);
        hello.wantsControl = res.value !== 0;
        offset = res.offset;
        break;
      }
      case 5: {
        const res = readVarint(bytes, offset);
        hello.lastSeq = res.value;
        offset = res.offset;
        break;
      }
      case 6: {
        const res = readString(bytes, offset);
        hello.clientType = res.value;
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported hello wire");
      }
    }
  }
  return hello;
}

function decodeWelcome(bytes) {
  const welcome = {
    grantedControl: false,
    serverCols: 0,
    serverRows: 0,
    holderClientId: "",
  };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readVarint(bytes, offset);
        welcome.grantedControl = res.value !== 0;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        welcome.serverCols = res.value;
        offset = res.offset;
        break;
      }
      case 3: {
        const res = readVarint(bytes, offset);
        welcome.serverRows = res.value;
        offset = res.offset;
        break;
      }
      case 4: {
        const res = readString(bytes, offset);
        welcome.holderClientId = res.value;
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported welcome wire");
      }
    }
  }
  return welcome;
}

function decodeControl(bytes) {
  let offset = 0;
  const out = { holderClientId: "" };
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      out.holderClientId = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported control wire");
  }
  return out;
}

function decodeResize(bytes) {
  const out = { cols: 0, rows: 0 };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      out.cols = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 2 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      out.rows = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported resize wire");
  }
  return out;
}

function decodeError(bytes) {
  let offset = 0;
  const out = { message: "", retryAfterSeconds: 0 };
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      out.message = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 2 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      out.retryAfterSeconds = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported error wire");
  }
  return out;
}

function decodeSessionInfo(bytes) {
  const info = { id: "", name: "", status: "", lastActiveUnix: 0 };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      info.id = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 2 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      info.name = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 3 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      info.status = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 4 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      info.lastActiveUnix = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported session info wire");
  }
  return info;
}

function decodeSessions(bytes) {
  const out = { sessions: [] };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      out.sessions.push(decodeSessionInfo(res.value));
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported sessions wire");
  }
  return out;
}

function decodeWall(bytes) {
  const out = { sender: "", message: "", timeoutSeconds: 0, sourceSessionName: "" };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    if (field === 1 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      out.sender = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 2 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      out.message = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 3 && wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      out.timeoutSeconds = res.value;
      offset = res.offset;
      continue;
    }
    if (field === 7 && wire === WIRE_BYTES) {
      const res = readString(bytes, offset);
      out.sourceSessionName = res.value;
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_BYTES) {
      const res = readBytes(bytes, offset);
      offset = res.offset;
      continue;
    }
    if (wire === WIRE_VARINT) {
      const res = readVarint(bytes, offset);
      offset = res.offset;
      continue;
    }
    throw new Error("unsupported wall wire");
  }
  return out;
}

export function decodeFrame(buffer) {
  const bytes = buffer instanceof Uint8Array ? buffer : new Uint8Array(buffer);
  const frame = { sessionId: "", seq: 0, payload: null };
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.offset;
    const field = tag.value >>> 3;
    const wire = tag.value & 0x7;
    switch (field) {
      case 1: {
        const res = readString(bytes, offset);
        frame.sessionId = res.value;
        offset = res.offset;
        break;
      }
      case 2: {
        const res = readVarint(bytes, offset);
        frame.seq = res.value;
        offset = res.offset;
        break;
      }
      case 10: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "hello", data: decodeHello(res.value) };
        offset = res.offset;
        break;
      }
      case 11: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "welcome", data: decodeWelcome(res.value) };
        offset = res.offset;
        break;
      }
      case 12: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "snapshot", data: decodeSnapshot(res.value) };
        offset = res.offset;
        break;
      }
      case 13: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "diff", data: decodeDiff(res.value) };
        offset = res.offset;
        break;
      }
      case 14: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "out", data: res.value };
        offset = res.offset;
        break;
      }
      case 15: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "in", data: res.value };
        offset = res.offset;
        break;
      }
      case 16: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "resize", data: decodeResize(res.value) };
        offset = res.offset;
        break;
      }
      case 17: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "control", data: decodeControl(res.value) };
        offset = res.offset;
        break;
      }
      case 18: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "error", data: decodeError(res.value) };
        offset = res.offset;
        break;
      }
      case 19: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "scrollback", data: decodeScrollback(res.value) };
        offset = res.offset;
        break;
      }
      case 20: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "sessions", data: decodeSessions(res.value) };
        offset = res.offset;
        break;
      }
      case 21: {
        const res = readBytes(bytes, offset);
        frame.payload = { type: "wall", data: decodeWall(res.value) };
        offset = res.offset;
        break;
      }
      default: {
        if (wire === WIRE_BYTES) {
          const res = readBytes(bytes, offset);
          offset = res.offset;
          break;
        }
        if (wire === WIRE_VARINT) {
          const res = readVarint(bytes, offset);
          offset = res.offset;
          break;
        }
        throw new Error("unsupported frame wire");
      }
    }
  }
  return frame;
}

class Writer {
  constructor() {
    this.parts = [];
  }

  writeVarint(value) {
    let v = value >>> 0;
    while (v >= 0x80) {
      this.parts.push((v & 0x7f) | 0x80);
      v >>>= 7;
    }
    this.parts.push(v);
  }

  writeBytes(bytes) {
    this.writeVarint(bytes.length);
    for (const b of bytes) {
      this.parts.push(b);
    }
  }

  writeString(value) {
    const bytes = new TextEncoder().encode(value);
    this.writeBytes(bytes);
  }

  writeTag(field, wire) {
    this.writeVarint((field << 3) | wire);
  }

  finish() {
    return new Uint8Array(this.parts);
  }
}

function encodeHelloMessage(msg) {
  const w = new Writer();
  if (msg.clientId) {
    w.writeTag(1, WIRE_BYTES);
    w.writeString(msg.clientId);
  }
  w.writeTag(2, WIRE_VARINT);
  w.writeVarint(msg.cols >>> 0);
  w.writeTag(3, WIRE_VARINT);
  w.writeVarint(msg.rows >>> 0);
  w.writeTag(4, WIRE_VARINT);
  w.writeVarint(msg.wantsControl ? 1 : 0);
  if (msg.lastSeq) {
    w.writeTag(5, WIRE_VARINT);
    w.writeVarint(msg.lastSeq >>> 0);
  }
  if (msg.clientType) {
    w.writeTag(6, WIRE_BYTES);
    w.writeString(msg.clientType);
  }
  return w.finish();
}

function encodeResizeMessage(msg) {
  const w = new Writer();
  w.writeTag(1, WIRE_VARINT);
  w.writeVarint(msg.cols >>> 0);
  w.writeTag(2, WIRE_VARINT);
  w.writeVarint(msg.rows >>> 0);
  return w.finish();
}

function encodeInMessage(data) {
  const w = new Writer();
  w.writeTag(1, WIRE_BYTES);
  w.writeBytes(data instanceof Uint8Array ? data : new TextEncoder().encode(data));
  return w.finish();
}

export function encodeFrameHello({ sessionId, hello }) {
  const w = new Writer();
  if (sessionId) {
    w.writeTag(1, WIRE_BYTES);
    w.writeString(sessionId);
  }
  w.writeTag(10, WIRE_BYTES);
  w.writeBytes(encodeHelloMessage(hello));
  return w.finish();
}

export function encodeFrameIn(data) {
  const w = new Writer();
  w.writeTag(15, WIRE_BYTES);
  w.writeBytes(encodeInMessage(data));
  return w.finish();
}

export function encodeFrameResize({ cols, rows }) {
  const w = new Writer();
  w.writeTag(16, WIRE_BYTES);
  w.writeBytes(encodeResizeMessage({ cols, rows }));
  return w.finish();
}

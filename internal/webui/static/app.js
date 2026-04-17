import { decodeFrame, encodeFrameHello, encodeFrameIn } from "./proto.js";
import { applyDiffToSnapshot, renderSnapshot, setGraphemeWidthProvider } from "./renderer.js";

const loginView = document.getElementById("login-view");
const terminalView = document.getElementById("terminal-view");
const loginForm = document.getElementById("login-form");
const loginError = document.getElementById("login-error");
const shareTokenInput = document.getElementById("share-token-input");
const loginSubmitButton = document.getElementById("login-submit-btn");
const usernameInput = loginForm ? loginForm.elements.namedItem("username") : null;
const passwordInput = loginForm ? loginForm.elements.namedItem("password") : null;
const totpInput = loginForm ? loginForm.elements.namedItem("totp") : null;
const menuToggle = document.getElementById("menu-toggle");
const refreshSessionsButton = document.getElementById("refresh-sessions-btn");
const menu = document.getElementById("menu");
const listIcon = document.querySelector(".icon-list");
const closeIcon = document.querySelector(".icon-close");
const themeSelect = document.getElementById("theme-select");
const fontScaleInput = document.getElementById("font-scale");
const fontScaleLabel = document.getElementById("font-scale-label");
const fullscreenToggle = document.getElementById("fullscreen-toggle");
const logoutButton = document.getElementById("logout-btn");
const statusBanner = document.getElementById("status-banner");
const emptyState = document.getElementById("empty-state");
const connectionPill = document.getElementById("connection-pill");
const termGrid = document.getElementById("term-container");
const terminalShell = document.querySelector(".terminal-shell");
const scrollTrack = document.querySelector(".terminal-scrollbar");
const scrollThumb = document.querySelector(".terminal-scrollbar-thumb");
const tabBar = document.getElementById("tab-bar");
const tabList = document.getElementById("tab-list");

const baseURL = new URL(document.baseURI);
const bootstrapShareToken = consumeBootstrapShareTokenFromURL();

const terminalTheme = {
  background: "#000000",
  foreground: "#f5f7ff",
  cursor: "#7dcfff",
  black: "#000000",
  brightBlack: "#000000",
};

const defaultScrollbackLines = 5000;
const scrollThumbMinPx = 24;
const defaultCols = 80;
const defaultRows = 24;
const baseFontSize = 16;
const tileGapPx = 16;
const labelFallbackHeight = 16;

let layoutFrame = null;
let termPadding = null;
let graphemeProviderSet = false;
let sessions = [];
let activeSessionId = "";
let sessionRefreshTimer = null;
let wallPollTimer = null;
let wallPollInFlight = false;
let wallPollCursor = 0;
let wallPollFastUntil = 0;
let wallPollGeneration = 0;
let sessionsAvailable = true;
let inactiveSweepTimer = null;
let pendingFocus = false;
let fullscreenSingle = false;
let fullscreenActive = false;
const views = new Map();

const backoffPolicy = {
  base: 1000,
  factor: 2,
  max: 3 * 60 * 1000,
};

const backoffResetDelay = 5 * 1000;
const inactiveTTL = 30 * 1000;
const sessionRefreshInterval = 60 * 1000;
const wallPollFastInterval = 60 * 1000;
const wallPollSlowInterval = 15 * 60 * 1000;
const wallPollFastWindow = 15 * 60 * 1000;
const wallEventsPageLimit = 100;
const wallDedupeWindow = 30 * 1000;

const fontState = {
  scale: 1,
};

function resetSessionStreamBackoff() {}

if (typeof window !== "undefined") {
  window.__bifrons = window.__bifrons || {};
  window.__bifrons.debug = window.__bifrons.debug || {};
  window.__bifrons.debugInfo = () => ({
    debug: window.__bifrons.debug || {},
    keys: window.__bifrons.debug ? Object.keys(window.__bifrons.debug) : [],
  });
  window.addEventListener("error", (event) => {
    if (!window.__bifrons || !window.__bifrons.debug) {
      return;
    }
    window.__bifrons.debug.lastJSError = {
      message: event && event.message ? event.message : "error",
      at: Date.now(),
    };
  });
  window.addEventListener("unhandledrejection", (event) => {
    if (!window.__bifrons || !window.__bifrons.debug) {
      return;
    }
    const reason = event && event.reason ? String(event.reason) : "unhandledrejection";
    window.__bifrons.debug.lastPromiseError = {
      message: reason,
      at: Date.now(),
    };
  });
  window.__bifrons.viewInfo = () => {
    const view = getActiveView();
    if (!view) {
      return null;
    }
    return {
      reconnectAttempt: view.reconnectAttempt || 0,
      reconnectPending: !!view.reconnectTimer,
      wsState: view.ws ? view.ws.readyState : null,
    };
  };
  window.__bifrons.forceDisconnect = () => {
    const view = getActiveView();
    if (view && view.ws) {
      view.ws.close();
    }
    if (view) {
      view.reconnectAttempt = (view.reconnectAttempt || 0) + 1;
      scheduleReconnect(view, "connection lost");
    }
    if (window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastReconnectAt = Date.now();
    }
  };
  window.__bifrons.debug = window.__bifrons.debug || {};
  window.__bifrons.debug.setFullscreenActive = (active) => {
    fullscreenActive = !!active;
    applyFullscreenSingleUi();
    renderTabs();
    scheduleLayout();
  };
  window.__bifrons.debug.setFullscreenSingle = (enabled) => {
    fullscreenSingle = !!enabled;
    if (fullscreenToggle) {
      fullscreenToggle.checked = fullscreenSingle;
    }
    localStorage.setItem("bifrons.fullscreenSingle", fullscreenSingle ? "1" : "0");
    renderTabs();
    scheduleLayout();
  };
}

function updateDebugState() {
  if (typeof window === "undefined" || !window.__bifrons || !window.__bifrons.debug) {
    return;
  }
  window.__bifrons.debug.state = {
    authFailed,
    authRefreshing,
    sessionsAvailable,
    sessionsCount: Array.isArray(sessions) ? sessions.length : 0,
    activeSessionId: activeSessionId || "",
    lastSessionsAt,
    lastSessionsError,
  };
}

const scrollDrag = {
  active: false,
  pointerId: null,
  startY: 0,
  startTop: 0,
  viewId: "",
};

let authRefreshing = false;
let authFailureReason = "";
let authFailed = false;
let lastSessionsAt = 0;
let lastSessionsError = "";
let notificationPermissionRequested = false;
const recentWallNotifications = new Map();

function createView(sessionId) {
  return {
    sessionId,
    shareSession: false,
    wantsControl: true,
    ws: null,
    snapshot: null,
    term: null,
    fitAddon: null,
    tile: null,
    termContainer: null,
    labelEl: null,
    serverCols: 0,
    serverRows: 0,
    pendingResize: false,
    lastRenderCols: 0,
    lastRenderRows: 0,
    scrollbackRows: [],
    scrollbackCols: 0,
    scrollbackOffset: 0,
    scrollbackLimit: defaultScrollbackLines,
    reconnectAttempt: 0,
    reconnectTimer: null,
    countdownTimer: null,
    stableTimer: null,
    loadingTimer: null,
    loadingVisible: false,
    loadingText: "",
    suppressReconnect: false,
    ready: false,
    visible: false,
    hiddenAt: 0,
    noHost: false,
    connectionState: "offline",
    statusLevel: "error",
    statusText: "",
    queuedInput: "",
  };
}

function getActiveView() {
  if (!activeSessionId) {
    return null;
  }
  return views.get(activeSessionId) || null;
}

function viewRows(view) {
  if (!view) return 0;
  if (view.snapshot && view.snapshot.rows) {
    return view.snapshot.rows;
  }
  return view.serverRows || 0;
}

function viewTotalRows(view) {
  if (!view) return 0;
  const liveRows = viewRows(view);
  return (view.scrollbackRows ? view.scrollbackRows.length : 0) + liveRows;
}

function maxScrollbackOffset(view) {
  const rows = viewRows(view);
  const total = viewTotalRows(view);
  if (rows <= 0) {
    return 0;
  }
  return Math.max(0, total - rows);
}

function clampScrollbackOffset(view, offset) {
  if (!view) return 0;
  const maxOffset = maxScrollbackOffset(view);
  if (offset < 0) return 0;
  if (offset > maxOffset) return maxOffset;
  return offset;
}

function buildScrollbackSnapshot(view) {
  const live = view ? view.snapshot : null;
  if (!live) {
    return null;
  }
  const cols = live.cols || 0;
  const rows = live.rows || 0;
  if (!cols || !rows) {
    return live;
  }
  const scrollback = view.scrollbackRows || [];
  const totalRows = scrollback.length + rows;
  let offset = clampScrollbackOffset(view, view.scrollbackOffset || 0);
  let start = totalRows - rows - offset;
  if (start < 0) {
    start = 0;
  }

  const size = cols * rows;
  const runes = new Array(size).fill(0);
  const modes = new Array(size).fill(0);
  const fg = new Array(size).fill(0);
  const bg = new Array(size).fill(0);
  let graphemes = live.graphemes && live.graphemes.length > 0 ? new Array(size).fill("") : [];
  let hasGraphemes = false;

  const fillFromRow = (dstRow, row) => {
    if (!row) return;
    const base = dstRow * cols;
    const rowRunes = row.runes || [];
    const rowModes = row.modes || [];
    const rowFg = row.fg || [];
    const rowBg = row.bg || [];
    const rowGraphemes = row.graphemes || [];
    for (let x = 0; x < cols; x += 1) {
      const idx = base + x;
      if (x < rowRunes.length) runes[idx] = rowRunes[x];
      if (x < rowModes.length) modes[idx] = rowModes[x];
      if (x < rowFg.length) fg[idx] = rowFg[x];
      if (x < rowBg.length) bg[idx] = rowBg[x];
      if (graphemes.length > 0 && x < rowGraphemes.length) {
        graphemes[idx] = rowGraphemes[x];
        if (rowGraphemes[x]) {
          hasGraphemes = true;
        }
      }
    }
  };

  for (let viewRow = 0; viewRow < rows; viewRow += 1) {
    const sourceRow = start + viewRow;
    if (sourceRow < scrollback.length) {
      fillFromRow(viewRow, scrollback[sourceRow]);
      continue;
    }
    const liveRow = sourceRow - scrollback.length;
    if (liveRow < 0 || liveRow >= rows) {
      continue;
    }
    const srcBase = liveRow * cols;
    const dstBase = viewRow * cols;
    for (let x = 0; x < cols; x += 1) {
      const src = srcBase + x;
      const dst = dstBase + x;
      if (src < live.runes.length) runes[dst] = live.runes[src];
      if (src < live.modes.length) modes[dst] = live.modes[src];
      if (src < live.fg.length) fg[dst] = live.fg[src];
      if (src < live.bg.length) bg[dst] = live.bg[src];
      if (graphemes.length > 0 && src < live.graphemes.length) {
        graphemes[dst] = live.graphemes[src];
        if (live.graphemes[src]) {
          hasGraphemes = true;
        }
      }
    }
  }

  if (!hasGraphemes) {
    graphemes = [];
  }

  let cursor = null;
  let cursorVisible = false;
  if (live.cursor && offset === 0) {
    const cursorRow = scrollback.length + live.cursor.y;
    if (cursorRow >= start && cursorRow < start + rows) {
      cursor = { x: live.cursor.x, y: cursorRow - start };
      cursorVisible = live.cursorVisible;
    }
  }

  return {
    cols,
    rows,
    runes,
    modes,
    fg,
    bg,
    graphemes,
    cursor,
    cursorVisible,
    mode: live.mode,
    title: live.title,
  };
}

function updateScrollbar(view) {
  if (!scrollTrack || !scrollThumb) {
    return;
  }
  if (!view || !view.snapshot) {
    scrollTrack.classList.add("is-hidden");
    return;
  }
  const rows = viewRows(view);
  const total = viewTotalRows(view);
  const maxOffset = maxScrollbackOffset(view);
  if (rows <= 0 || total <= rows || maxOffset <= 0) {
    scrollTrack.classList.add("is-hidden");
    return;
  }
  scrollTrack.classList.remove("is-hidden");
  const trackHeight = scrollTrack.clientHeight || 0;
  if (trackHeight <= 0) {
    return;
  }
  const thumbHeight = Math.max(scrollThumbMinPx, Math.round((trackHeight * rows) / total));
  const maxTop = Math.max(0, trackHeight - thumbHeight);
  const offset = clampScrollbackOffset(view, view.scrollbackOffset || 0);
  const top = maxOffset > 0 ? Math.round((maxTop * (maxOffset - offset)) / maxOffset) : 0;
  scrollThumb.style.height = `${thumbHeight}px`;
  scrollThumb.style.top = `${top}px`;
}

function setScrollbackOffset(view, offset, forceRender = false) {
  if (!view) return;
  const next = clampScrollbackOffset(view, offset);
  if (next === view.scrollbackOffset && !forceRender) {
    return;
  }
  view.scrollbackOffset = next;
  if (view === getActiveView()) {
    renderView(view, forceRender);
    updateScrollbar(view);
  }
}

function scrollMetrics(view) {
  if (!scrollTrack || !scrollThumb || !view) return null;
  const rows = viewRows(view);
  const total = viewTotalRows(view);
  const maxOffset = maxScrollbackOffset(view);
  const trackRect = scrollTrack.getBoundingClientRect();
  const trackHeight = scrollTrack.clientHeight || 0;
  if (!rows || !total || !trackHeight || maxOffset <= 0) {
    return null;
  }
  const thumbHeight = Math.max(scrollThumbMinPx, Math.round((trackHeight * rows) / total));
  const maxTop = Math.max(0, trackHeight - thumbHeight);
  const offset = clampScrollbackOffset(view, view.scrollbackOffset || 0);
  const thumbTop = maxOffset > 0 ? (maxTop * (maxOffset - offset)) / maxOffset : 0;
  return {
    trackTop: trackRect.top,
    trackHeight,
    thumbHeight,
    maxTop,
    maxOffset,
    thumbTop,
  };
}

function sessionExists(sessionId) {
  return sessions.some((s) => s.id === sessionId);
}

function canReconnect(view) {
  if (!view) {
    return false;
  }
  if (view.sessionId && views.get(view.sessionId) !== view) {
    return false;
  }
  if (authFailed) {
    return false;
  }
  if (!sessionsAvailable) {
    return true;
  }
  return sessionExists(view.sessionId);
}

function ensureView(sessionId) {
  if (!sessionId) {
    return null;
  }
  let view = views.get(sessionId);
  if (!view) {
    view = createView(sessionId);
    views.set(sessionId, view);
  }
  return view;
}

function sessionLabelById(sessionId) {
  if (!sessionId) {
    return "";
  }
  const match = sessions.find((s) => s.id === sessionId);
  return match ? match.name || match.id : sessionId;
}

function ensureViewDom(view) {
  if (!view || view.tile) {
    return;
  }
  const tile = document.createElement("div");
  tile.className = "terminal-tile";
  tile.dataset.sessionId = view.sessionId || "";
  const container = document.createElement("div");
  container.className = "terminal-container";
  const label = document.createElement("div");
  label.className = "terminal-label";
  label.textContent = sessionLabelById(view.sessionId);
  tile.appendChild(container);
  tile.appendChild(label);
  if (termGrid) {
    termGrid.appendChild(tile);
  }
  view.tile = tile;
  view.termContainer = container;
  view.labelEl = label;
}

function destroyView(view) {
  if (!view) {
    return;
  }
  disconnectView(view);
  if (view.term && typeof view.term.dispose === "function") {
    view.term.dispose();
  }
  view.term = null;
  view.fitAddon = null;
  if (view.tile && view.tile.parentElement) {
    view.tile.parentElement.removeChild(view.tile);
  }
  view.tile = null;
  view.termContainer = null;
  view.labelEl = null;
}

function viewDimensions(view) {
  if (!view) {
    return { cols: defaultCols, rows: defaultRows };
  }
  const cols = view.serverCols || (view.snapshot && view.snapshot.cols) || defaultCols;
  const rows = view.serverRows || (view.snapshot && view.snapshot.rows) || defaultRows;
  return { cols, rows };
}

function updateViewLabel(view) {
  if (!view || !view.labelEl) {
    return;
  }
  view.labelEl.textContent = sessionLabelById(view.sessionId);
}

function stopReconnectTimers(view) {
  if (!view) {
    return;
  }
  if (view.reconnectTimer) {
    clearTimeout(view.reconnectTimer);
    view.reconnectTimer = null;
  }
  if (view.countdownTimer) {
    clearInterval(view.countdownTimer);
    view.countdownTimer = null;
  }
  if (view.stableTimer) {
    clearTimeout(view.stableTimer);
    view.stableTimer = null;
  }
}

function disconnectView(view) {
  if (!view) {
    return;
  }
  stopReconnectTimers(view);
  clearConnectedBannerTimer(view);
  view.loadingVisible = false;
  view.loadingText = "";
  view.suppressReconnect = true;
  view.ready = false;
  if (view.ws) {
    view.ws.close();
  }
  view.ws = null;
  view.connectionState = "offline";
  view.statusText = "";
  view.statusLevel = "error";
}

function disconnectAllViews() {
  for (const view of views.values()) {
    disconnectView(view);
  }
}

function queueInput(view, data) {
  if (!view || !data) {
    return;
  }
  const max = 64 * 1024;
  const combined = view.queuedInput + data;
  view.queuedInput = combined.length > max ? combined.slice(-max) : combined;
}

function setLoginReason(reason) {
  window.__bifrons = window.__bifrons || {};
  window.__bifrons.lastLoginReason = reason || "";
}

function forceInputTheme(input) {
  if (!input) {
    return;
  }
  input.style.setProperty("background-color", "var(--panel)", "important");
  input.style.setProperty("background-image", "none", "important");
  input.style.setProperty("color", "var(--text)", "important");
  input.style.setProperty("-webkit-text-fill-color", "var(--text)", "important");
  input.style.setProperty("box-shadow", "0 0 0 1000px var(--panel) inset", "important");
  input.style.setProperty("-webkit-box-shadow", "0 0 0 1000px var(--panel) inset", "important");
}

function enforceLoginInputTheme() {
  forceInputTheme(usernameInput);
  forceInputTheme(passwordInput);
  forceInputTheme(totpInput);
  forceInputTheme(shareTokenInput);
}

function clearLoginForm() {
  if (!loginForm) {
    return;
  }
  if (typeof loginForm.reset === "function") {
    loginForm.reset();
  }
  if (usernameInput) {
    usernameInput.value = "";
  }
  if (passwordInput) {
    passwordInput.value = "";
  }
  if (totpInput) {
    totpInput.value = "";
  }
  if (shareTokenInput) {
    shareTokenInput.value = "";
  }
  loginError.classList.add("hidden");
  loginError.textContent = "";
  updateLoginModeForToken();
  enforceLoginInputTheme();
  requestAnimationFrame(() => {
    enforceLoginInputTheme();
  });
  window.setTimeout(() => {
    enforceLoginInputTheme();
  }, 60);
}

function showLogin(reason) {
  setLoginReason(reason);
  clearLoginForm();
  loginView.classList.remove("hidden");
  terminalView.classList.add("hidden");
  stopSessionRefresh();
  stopInactiveSweep();
  resetSessionStreamBackoff();
  for (const view of views.values()) {
    destroyView(view);
  }
  views.clear();
  sessions = [];
  activeSessionId = "";
  if (termGrid) {
    termGrid.innerHTML = "";
  }
  tabBar.classList.add("hidden");
  document.body.classList.remove("has-tabs");
  setStatusBanner("");
  setEmptyState(false);
  setConnectionPill("offline");
  updateDebugState();
}

function showTerminal() {
  setLoginReason("");
  loginView.classList.add("hidden");
  terminalView.classList.remove("hidden");
  maybeRequestNotificationPermission();
  if (Array.isArray(sessions) && sessions.length === 1) {
    requestFocusActiveView();
  }
  updateDebugState();
}

function maybeRequestNotificationPermission() {
  if (typeof window === "undefined" || typeof Notification === "undefined") {
    return;
  }
  if (Notification.permission !== "default" || notificationPermissionRequested) {
    return;
  }
  notificationPermissionRequested = true;
  Promise.resolve(Notification.requestPermission()).catch(() => {});
}

function wallNotificationKey(sender, message) {
  return `${sender}\n${message}`;
}

function pruneRecentWallNotifications(nowMs) {
  for (const [key, ts] of recentWallNotifications.entries()) {
    if (nowMs - ts > wallDedupeWindow) {
      recentWallNotifications.delete(key);
    }
  }
}

function shouldSuppressWallNotification(sender, message, eventMs) {
  const nowMs = Date.now();
  pruneRecentWallNotifications(nowMs);
  const key = wallNotificationKey(sender, message);
  const seenAt = recentWallNotifications.get(key);
  if (typeof seenAt === "number" && Math.abs(eventMs - seenAt) <= wallDedupeWindow) {
    return true;
  }
  recentWallNotifications.set(key, eventMs);
  return false;
}

function formatWallSource(sender, sessionName) {
  const cleanSender = String(sender || "").trim();
  const cleanSession = String(sessionName || "").trim();
  if (!cleanSender) {
    return cleanSession;
  }
  if (!cleanSession) {
    return cleanSender;
  }
  return `${cleanSender}#${cleanSession}`;
}

function showWallNotification(data) {
  if (typeof window === "undefined" || typeof Notification === "undefined") {
    return;
  }
  const sender = formatWallSource(
    data && data.sender ? String(data.sender) : "",
    data && (data.source_session_name || data.sourceSessionName) ? String(data.source_session_name || data.sourceSessionName) : "",
  );
  const title = "Broadcast";
  const message = data && data.message ? String(data.message).trim() : "";
  const body = sender && message ? `${sender}: ${message}` : sender || message;
  const eventMs =
    data && data.created_at && !Number.isNaN(Date.parse(data.created_at))
      ? Date.parse(data.created_at)
      : Date.now();
  if (shouldSuppressWallNotification(sender, body, eventMs)) {
    return;
  }
  const timeoutSeconds = data && data.timeoutSeconds ? Math.max(1, Number(data.timeoutSeconds) || 0) : 5;
  if (Notification.permission === "granted") {
    try {
      const note = new Notification(title, { body });
      setTimeout(() => {
        if (note && typeof note.close === "function") {
          note.close();
        }
      }, timeoutSeconds * 1000);
      return;
    } catch {}
  }
  if (Notification.permission === "default") {
    maybeRequestNotificationPermission();
  }
}

function normalizedShareTokenInput() {
  if (!shareTokenInput) {
    return "";
  }
  return String(shareTokenInput.value || "").trim();
}

function extractShareToken(rawValue) {
  const value = String(rawValue || "").trim();
  if (!value) {
    return "";
  }
  try {
    const parsed = new URL(value);
    const token = parsed.searchParams.get("token");
    if (token) {
      return token.trim();
    }
  } catch {}
  return value;
}

function consumeBootstrapShareTokenFromURL() {
  let parsed;
  try {
    parsed = new URL(window.location.href);
  } catch {
    return "";
  }
  const raw = parsed.searchParams.get("token");
  if (!raw) {
    return "";
  }
  const token = extractShareToken(raw);
  parsed.searchParams.delete("token");
  const scrubbed = `${parsed.pathname}${parsed.search}${parsed.hash}`;
  if (window.history && typeof window.history.replaceState === "function") {
    window.history.replaceState({}, "", scrubbed);
  }
  return token;
}

function setFieldEnabled(input, enabled) {
  if (!input) {
    return;
  }
  input.disabled = !enabled;
  input.required = enabled;
  const field = input.closest(".field");
  if (field) {
    field.classList.toggle("is-disabled", !enabled);
  }
}

function updateLoginModeForToken() {
  const hasToken = normalizedShareTokenInput() !== "";
  setFieldEnabled(usernameInput, !hasToken);
  setFieldEnabled(passwordInput, !hasToken);
  setFieldEnabled(totpInput, !hasToken);
  if (loginSubmitButton) {
    loginSubmitButton.disabled = false;
    loginSubmitButton.textContent = hasToken ? "Attach with token" : "Connect";
  }
}

function startShareSession(sessionInfo) {
  const shareSessionId = (sessionInfo && sessionInfo.session_id) || "shared";
  const shareSessionName = (sessionInfo && sessionInfo.name) || "Shared session";
  const scope = String((sessionInfo && sessionInfo.scope) || "").toLowerCase();
  showLogin("share-token");
  sessions = [{ id: shareSessionId, name: shareSessionName }];
  activeSessionId = shareSessionId;
  const view = ensureView(shareSessionId);
  view.shareSession = true;
  view.wantsControl = scope !== "view";
  view.visible = true;
  showTerminal();
  renderTabs();
  updateActiveStatus();
  connectView(view);
  scheduleLayout();
}

function markViewRecovered(view) {
  if (!view) {
    return;
  }
  stopReconnectTimers(view);
  view.reconnectAttempt = 0;
  view.noHost = false;
  view.statusText = "";
  view.statusLevel = "error";
  setViewConnectionState(view, "online");
  if (view === getActiveView()) {
    updateActiveStatus();
    setEmptyState(false);
  }
}

async function attachWithShareToken(rawToken) {
  loginError.classList.add("hidden");
  const token = extractShareToken(rawToken);
  if (!token) {
    loginError.textContent = "Share token is required.";
    loginError.classList.remove("hidden");
    return false;
  }
  let resp;
  try {
    resp = await fetch("auth/share", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
  } catch {
    loginError.textContent = "Unable to attach with token.";
    loginError.classList.remove("hidden");
    return false;
  }
  if (!resp.ok) {
    loginError.textContent = "Share token is invalid or expired.";
    loginError.classList.remove("hidden");
    return false;
  }
  const sessionInfo = await resp.json();
  startShareSession(sessionInfo);
  return true;
}

async function restoreShareSession() {
  let resp;
  try {
    resp = await fetch("auth/share/session", { credentials: "include" });
  } catch {
    return false;
  }
  if (!resp.ok) {
    return false;
  }
  const sessionInfo = await resp.json();
  startShareSession(sessionInfo);
  return true;
}

function setStatusBanner(text, level = "error") {
  if (!text) {
    statusBanner.classList.add("hidden");
    statusBanner.textContent = "";
    statusBanner.dataset.level = "";
    statusBanner.classList.remove("is-loading", "is-warn", "is-info", "is-error");
    return;
  }
  statusBanner.textContent = text;
  statusBanner.dataset.level = level;
  statusBanner.classList.toggle("is-loading", level === "loading");
  statusBanner.classList.toggle("is-warn", level === "warn");
  statusBanner.classList.toggle("is-info", level === "info");
  statusBanner.classList.toggle("is-error", level === "error");
  statusBanner.classList.remove("hidden");
}

function setEmptyState(show) {
  if (show) {
    emptyState.classList.remove("hidden");
  } else {
    emptyState.classList.add("hidden");
  }
}

function setConnectionPill(state) {
  connectionPill.textContent = state;
  if (state === "online") {
    connectionPill.style.color = "var(--success)";
  } else if (state === "connecting") {
    connectionPill.style.color = "var(--info)";
  } else {
    connectionPill.style.color = "var(--text-muted)";
  }
}

function updateActiveTileClasses() {
  for (const view of views.values()) {
    if (!view || !view.tile) {
      continue;
    }
    view.tile.classList.toggle("is-active", view.sessionId === activeSessionId);
  }
}

function updateActiveStatus() {
  const view = getActiveView();
  if (!view) {
    setStatusBanner("");
    setEmptyState(sessions.length === 0);
    setConnectionPill("offline");
    updateScrollbar(null);
    updateActiveTileClasses();
    return;
  }
  if (typeof window !== "undefined") {
    window.__bifrons = window.__bifrons || {};
    window.__bifrons.term = view.term || null;
  }
  setConnectionPill(view.connectionState || "offline");
  if (view.statusText) {
    setStatusBanner(view.statusText, view.statusLevel || "error");
  } else if (view.loadingVisible) {
    setStatusBanner(view.loadingText || "loading from relay", "loading");
  } else {
    setStatusBanner("");
  }
  const showEmpty = view.noHost || !view.snapshot;
  setEmptyState(showEmpty);
  updateScrollbar(view);
  updateActiveTileClasses();
}

function focusActiveView() {
  const view = getActiveView();
  if (!view || !view.term) {
    return;
  }
  if (typeof view.term.focus === "function") {
    view.term.focus();
    return;
  }
  if (view.term.textarea && typeof view.term.textarea.focus === "function") {
    view.term.textarea.focus();
  }
}

function requestFocusActiveView() {
  pendingFocus = true;
  scheduleLayout();
}

function shouldAutoFocusSingle() {
  if (!Array.isArray(sessions) || sessions.length !== 1) {
    return false;
  }
  if (!terminalView || terminalView.classList.contains("hidden")) {
    return false;
  }
  const active = document.activeElement;
  if (!active || active === document.body) {
    return true;
  }
  return (
    active === terminalView ||
    active === terminalShell ||
    active === termGrid
  );
}

function isFullscreenSingleMode() {
  return fullscreenActive && fullscreenSingle;
}

function applyFullscreenSingleUi() {
  document.body.classList.toggle("fullscreen-single", isFullscreenSingleMode());
}

function setViewConnectionState(view, state) {
  if (!view) {
    return;
  }
  view.connectionState = state;
  if (view === getActiveView()) {
    setConnectionPill(state);
  }
  updateDebugState();
}

function scheduleReconnect(view, reason, retryAfterSeconds = 0) {
  if (!view || !view.visible || !canReconnect(view)) {
    return;
  }
  if (view.reconnectTimer) {
    return;
  }
  if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
    window.__bifrons.debug.lastReconnectReason = reason || "";
    window.__bifrons.debug.lastReconnectAt = Date.now();
  }
  stopReconnectTimers(view);
  clearConnectedBannerTimer(view);
  view.loadingVisible = false;
  view.loadingText = "";
  view.reconnectAttempt += 1;
  const delay = Math.min(
    backoffPolicy.max,
    backoffPolicy.base * Math.pow(backoffPolicy.factor, view.reconnectAttempt - 1),
  );
  const retryDelay = retryAfterSeconds > 0 ? retryAfterSeconds * 1000 : 0;
  const finalDelay = Math.max(delay, retryDelay);
  let remaining = Math.ceil(delay / 1000);
  if (retryDelay > delay) {
    remaining = Math.ceil(finalDelay / 1000);
  }
  view.statusText = `${reason} (reconnecting ${remaining}s)`;
  view.statusLevel = "error";
  view.connectionState = "waiting";
  if (view === getActiveView()) {
    setStatusBanner(view.statusText, "error");
    setEmptyState(true);
    setConnectionPill("waiting");
  }

  view.countdownTimer = setInterval(() => {
    remaining -= 1;
    if (remaining <= 0) {
      clearInterval(view.countdownTimer);
      view.countdownTimer = null;
      return;
    }
    view.statusText = `${reason} (reconnecting ${remaining}s)`;
    if (view === getActiveView()) {
      setStatusBanner(view.statusText, "error");
    }
  }, 1000);

  view.reconnectTimer = setTimeout(() => {
    view.reconnectTimer = null;
    if (!canReconnect(view)) {
      return;
    }
    if (view.shareSession) {
      connectView(view);
      return;
    }
    // Refresh sessions before retrying so stale session IDs are dropped quickly.
    Promise.resolve(refreshSessionsGuarded("ws-reconnect"))
      .catch(() => {})
      .finally(() => {
        if (canReconnect(view) && !view.reconnectTimer) {
          connectView(view);
        }
      });
  }, finalDelay);
}

function resetReconnect(view) {
  if (!view) {
    return;
  }
  view.reconnectAttempt = 0;
  stopReconnectTimers(view);
  clearConnectedBannerTimer(view);
  view.statusText = "";
  view.statusLevel = "error";
  if (view === getActiveView()) {
    setStatusBanner("");
    updateActiveStatus();
  }
}

function scheduleBackoffReset(view) {
  if (!view || view.reconnectAttempt === 0 || view.stableTimer) {
    return;
  }
  view.stableTimer = setTimeout(() => {
    view.stableTimer = null;
    if (!view.ws || view.ws.readyState !== WebSocket.OPEN) {
      return;
    }
    resetReconnect(view);
  }, backoffResetDelay);
}

function measureCharMetrics(fontSize) {
  const span = document.createElement("span");
  span.style.position = "absolute";
  span.style.visibility = "hidden";
  span.style.whiteSpace = "pre";
  span.style.fontFamily = '"JetBrains Mono", "SFMono-Regular", "Menlo", "Consolas", monospace';
  span.style.fontSize = `${fontSize}px`;
  span.textContent = "W".repeat(20);
  document.body.appendChild(span);
  const rect = span.getBoundingClientRect();
  document.body.removeChild(span);
  return { width: rect.width / 20, height: rect.height };
}

function applyZoom() {
  // no-op: font scaling is applied via font size instead of CSS transforms
}

function setInitStep(step) {
  window.__bifrons = window.__bifrons || {};
  window.__bifrons.initStep = step;
}

function getElementPadding(el) {
  if (!el) {
    return { x: 0, y: 0 };
  }
  const styles = window.getComputedStyle(el);
  const px = (value) => {
    const n = Number.parseFloat(value || "0");
    return Number.isFinite(n) ? n : 0;
  };
  const paddingX = px(styles.paddingLeft) + px(styles.paddingRight);
  const paddingY = px(styles.paddingTop) + px(styles.paddingBottom);
  return { x: paddingX, y: paddingY };
}

function getShellInnerSize() {
  const shell = terminalShell || (termGrid ? termGrid.parentElement : null) || termGrid;
  const padding = getElementPadding(shell);
  let width = shell ? shell.clientWidth - padding.x : 0;
  let height = shell ? shell.clientHeight - padding.y : 0;
  if (width <= 0 || height <= 0) {
    const rect = shell ? shell.getBoundingClientRect() : { width: 0, height: 0 };
    width = rect.width - padding.x;
    height = rect.height - padding.y;
  }
  return {
    shell,
    width: Math.max(0, width),
    height: Math.max(0, height),
  };
}

function scheduleLayout() {
  if (layoutFrame) {
    return;
  }
  layoutFrame = requestAnimationFrame(() => {
    layoutFrame = null;
    layoutViews();
  });
}

function fitViewFontSize(view) {
  if (!view || !view.term || !view.serverCols || !view.serverRows) {
    return;
  }
  const finalSize = Math.max(8, Math.round(baseFontSize * fontState.scale));
  view.term.options.fontSize = finalSize;
  view.term.resize(view.serverCols, view.serverRows);
}

function getTermCellMetrics(view) {
  const core = view && view.term && view.term._core;
  const cell = core && core._renderService && core._renderService.dimensions && core._renderService.dimensions.css
    ? core._renderService.dimensions.css.cell
    : null;
  if (cell && cell.width && cell.height) {
    return { width: cell.width, height: cell.height };
  }
  return null;
}

function pickVisibleSessionIds(capacity) {
  if (!Array.isArray(sessions) || sessions.length === 0) {
    return [];
  }
  const ids = sessions.map((s) => s.id).filter((id) => id);
  const ordered = [];
  if (activeSessionId && ids.includes(activeSessionId)) {
    ordered.push(activeSessionId);
  }
  for (const id of ids) {
    if (!ordered.includes(id)) {
      ordered.push(id);
    }
  }
  const limit = Math.max(1, Math.min(capacity, ordered.length));
  return ordered.slice(0, limit);
}

function computeLayoutForIds(sessionIds, overrideSize) {
  const shellInfo = getShellInnerSize();
  if (!shellInfo || shellInfo.width <= 0 || shellInfo.height <= 0) {
    return { columns: 1, rows: 1, capacity: 1, tileWidth: 0, tileHeight: 0 };
  }
  const overrideWidth = overrideSize && overrideSize.width ? overrideSize.width : 0;
  const overrideHeight = overrideSize && overrideSize.height ? overrideSize.height : 0;
  const gridWidth =
    overrideWidth > 0 ? overrideWidth : termGrid ? termGrid.clientWidth || shellInfo.width : shellInfo.width;
  const gridHeight =
    overrideHeight > 0 ? overrideHeight : termGrid ? termGrid.clientHeight || shellInfo.height : shellInfo.height;
  let maxCols = defaultCols;
  let maxRows = defaultRows;
  if (Array.isArray(sessionIds)) {
    for (const id of sessionIds) {
      if (!id) continue;
      const view = views.get(id);
      const dims = viewDimensions(view);
      if (dims.cols > maxCols) maxCols = dims.cols;
      if (dims.rows > maxRows) maxRows = dims.rows;
    }
  }
  const metrics = measureCharMetrics(Math.max(8, Math.round(baseFontSize * fontState.scale)));
  const pad = getDefaultTermPadding();
  const tileWidth = Math.ceil(metrics.width * maxCols + pad.x + 8);
  const tileHeight = Math.ceil(metrics.height * maxRows + pad.y + labelFallbackHeight + 4);
  const columns = Math.max(1, Math.floor((gridWidth + tileGapPx) / (tileWidth + tileGapPx)));
  const rowsCount = Math.max(1, Math.floor((gridHeight + tileGapPx) / (tileHeight + tileGapPx)));
  return {
    columns,
    rows: rowsCount,
    capacity: Math.max(1, columns * rowsCount),
    tileWidth,
    tileHeight,
    gridWidth,
    gridHeight,
  };
}

function sameIdOrder(a, b) {
  if (a === b) return true;
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

function layoutViews() {
  if (!termGrid) {
    return;
  }
  applyFullscreenSingleUi();
  if (!Array.isArray(sessions) || sessions.length === 0) {
    for (const view of views.values()) {
      if (view.tile) {
        view.tile.classList.add("hidden");
      }
      view.visible = false;
    }
    return;
  }
  termGrid.style.overflow = "hidden";
  const allIds = sessions.map((s) => s.id).filter((id) => id);
  const fullscreenGridSize =
    isFullscreenSingleMode() && termGrid
      ? { width: termGrid.clientWidth, height: termGrid.clientHeight }
      : null;
  const overrideSize =
    fullscreenGridSize && fullscreenGridSize.width > 0 && fullscreenGridSize.height > 0
      ? fullscreenGridSize
      : null;
  let layout;
  let visibleIds;
  if (isFullscreenSingleMode()) {
    if (!activeSessionId && allIds.length > 0) {
      activeSessionId = allIds[0];
    }
    const singleId = activeSessionId || (allIds.length > 0 ? allIds[0] : "");
    visibleIds = singleId ? [singleId] : [];
    layout = computeLayoutForIds(visibleIds, overrideSize);
  } else {
    layout = computeLayoutForIds(allIds, overrideSize);
    visibleIds = pickVisibleSessionIds(layout.capacity);
    let singleFit = visibleIds.length === 1;
    for (let i = 0; i < 3; i += 1) {
      const nextLayout = computeLayoutForIds(visibleIds, overrideSize);
      const nextVisible = pickVisibleSessionIds(nextLayout.capacity);
      if (sameIdOrder(visibleIds, nextVisible)) {
        layout = nextLayout;
        visibleIds = nextVisible;
        singleFit = visibleIds.length === 1;
        break;
      }
      layout = nextLayout;
      visibleIds = nextVisible;
      singleFit = visibleIds.length === 1;
    }
  }
  const singleFit = visibleIds.length === 1;
  if (singleFit) {
    const singleWidth = isFullscreenSingleMode()
      ? layout.gridWidth
      : Math.min(layout.gridWidth, layout.tileWidth);
    const singleHeight = isFullscreenSingleMode()
      ? layout.gridHeight
      : layout.tileHeight;
    termGrid.style.gridTemplateColumns = `repeat(1, ${singleWidth}px)`;
    termGrid.style.gridTemplateRows = `repeat(1, ${singleHeight}px)`;
  } else {
    termGrid.style.gridTemplateColumns = `repeat(${layout.columns}, ${layout.tileWidth}px)`;
    termGrid.style.gridTemplateRows = `repeat(${layout.rows}, ${layout.tileHeight}px)`;
  }

  const visibleSet = new Set(visibleIds);

  for (const sessionId of visibleIds) {
    const view = ensureView(sessionId);
    ensureViewDom(view);
    updateViewLabel(view);
    if (view.tile) {
      view.tile.classList.remove("hidden");
    }
    view.visible = true;
    view.hiddenAt = 0;
    setupTerminalForView(view);
    if ((!view.ws || view.ws.readyState !== WebSocket.OPEN) && !view.reconnectTimer) {
      connectView(view);
    }
  }

  const collectFittingIds = () => {
    const gridRect = termGrid.getBoundingClientRect();
    const result = [];
    for (const sessionId of visibleIds) {
      const view = views.get(sessionId);
      if (!view || !view.tile) {
        continue;
      }
      const rect = view.tile.getBoundingClientRect();
      const overflowRight = rect.right > gridRect.right + 0.5;
      const overflowBottom = rect.bottom > gridRect.bottom + 0.5;
      if (overflowRight || overflowBottom) {
        view.tile.classList.add("hidden");
        view.visible = false;
        view.hiddenAt = Date.now();
        continue;
      }
      result.push(sessionId);
    }
    return result;
  };

  let fittingIds = [];
  const forceSingleVisible = isFullscreenSingleMode() || visibleIds.length === 1;
  if (forceSingleVisible) {
    fittingIds = visibleIds.slice();
  } else {
    fittingIds = collectFittingIds();
    if (fittingIds.length > 0 && fittingIds.length < visibleIds.length) {
      const tighter = computeLayoutForIds(fittingIds, overrideSize);
      termGrid.style.gridTemplateColumns = `repeat(${tighter.columns}, ${tighter.tileWidth}px)`;
      termGrid.style.gridTemplateRows = `repeat(${tighter.rows}, ${tighter.tileHeight}px)`;
      fittingIds = collectFittingIds();
    }
  }
  if (fittingIds.length === 0 && allIds.length > 0) {
    const candidateIds = visibleIds.length > 0 ? visibleIds : allIds;
    const fallbackId = activeSessionId && candidateIds.includes(activeSessionId)
      ? activeSessionId
      : candidateIds[0];
    fittingIds = fallbackId ? [fallbackId] : [];
  }
  const fittingSet = new Set(fittingIds);

  for (const sessionId of fittingIds) {
    const view = ensureView(sessionId);
    if (!view) {
      continue;
    }
    ensureViewDom(view);
    updateViewLabel(view);
    setupTerminalForView(view);
    if ((!view.ws || view.ws.readyState !== WebSocket.OPEN) && !view.reconnectTimer) {
      connectView(view);
    }
    if (view.tile) {
      view.tile.classList.remove("hidden");
    }
    view.visible = true;
    view.hiddenAt = 0;
  }

  for (const [id, view] of views.entries()) {
    if (fittingSet.has(id)) {
      continue;
    }
    if (view.tile) {
      view.tile.classList.add("hidden");
    }
    if (view.visible) {
      view.hiddenAt = Date.now();
    }
    view.visible = false;
  }

  for (const sessionId of fittingIds) {
    const view = views.get(sessionId);
    if (!view) {
      continue;
    }
    if (fittingIds.length === 1 && layout.gridWidth) {
      const cols = view.serverCols || (view.snapshot && view.snapshot.cols) || defaultCols;
      const rows = view.serverRows || (view.snapshot && view.snapshot.rows) || defaultRows;
      const pad = getDefaultTermPadding();
      const baseSize = Math.max(8, Math.round(baseFontSize * fontState.scale));
      const currentSize = view.term && view.term.options && view.term.options.fontSize
        ? view.term.options.fontSize
        : baseSize;
      const base = getTermCellMetrics(view) || measureCharMetrics(currentSize);
      const widthLimit = isFullscreenSingleMode() ? layout.gridWidth : Math.min(layout.gridWidth, layout.tileWidth);
      const heightLimit = isFullscreenSingleMode() ? layout.gridHeight : Math.min(layout.gridHeight, layout.tileHeight);
      const usableWidth = Math.max(0, widthLimit - pad.x);
      let fitFont = Math.floor((usableWidth / cols) / (base.width / currentSize));
      const measuredLabel = view.labelEl ? view.labelEl.offsetHeight : 0;
      const labelHeight = Math.max(labelFallbackHeight, measuredLabel);
      const usableHeight = Math.max(0, heightLimit - pad.y - labelHeight - 4);
      const fitByHeight = Math.floor((usableHeight / rows) / (base.height / currentSize));
      fitFont = Math.min(fitFont, fitByHeight);
      const maxSize = Math.max(8, Math.round(baseFontSize * fontState.scale));
      let finalSize = isFullscreenSingleMode()
        ? Math.max(8, fitFont)
        : Math.max(8, Math.min(fitFont, maxSize));
      for (let i = 0; i < 6; i += 1) {
        view.term.options.fontSize = finalSize;
        view.term.resize(cols, rows);
        const cell = getTermCellMetrics(view) || measureCharMetrics(finalSize);
        const tileHeight = Math.ceil(cell.height * rows + pad.y + labelHeight);
        if (tileHeight <= heightLimit - 2) {
          break;
        }
        if (finalSize <= 8) {
          break;
        }
        finalSize -= 1;
      }
      view.pendingResize = false;
      const metrics = getTermCellMetrics(view) || measureCharMetrics(finalSize);
      const tileWidth = Math.ceil(metrics.width * cols + pad.x);
      const tileHeight = Math.ceil(metrics.height * rows + pad.y + labelHeight);
      if (isFullscreenSingleMode()) {
        termGrid.style.gridTemplateColumns = `repeat(1, ${layout.gridWidth}px)`;
        termGrid.style.gridTemplateRows = `repeat(1, ${layout.gridHeight}px)`;
      } else {
        termGrid.style.gridTemplateColumns = `repeat(1, ${Math.min(layout.gridWidth, tileWidth)}px)`;
        termGrid.style.gridTemplateRows = `repeat(1, ${tileHeight}px)`;
      }
    }
    if (view.snapshot) {
      updateViewSize(view, view.snapshot.cols, view.snapshot.rows);
    } else if (view.serverCols && view.serverRows) {
      updateViewSize(view, view.serverCols, view.serverRows);
    }
    const pendingResize = view.pendingResize;
    if (fittingIds.length === 1) {
      view.pendingResize = false;
    } else if (pendingResize) {
      fitViewFontSize(view);
      view.pendingResize = false;
    }
    if (view.snapshot) {
      renderView(view, fittingIds.length === 1 ? true : pendingResize);
    }
  }
  updateActiveStatus();
  updateTabOverflowState();
  if (pendingFocus) {
    pendingFocus = false;
    requestAnimationFrame(() => {
      focusActiveView();
    });
  } else if (singleFit && shouldAutoFocusSingle()) {
    requestAnimationFrame(() => {
      focusActiveView();
    });
  }
}

function getTermPaddingFor(el) {
  if (!el) {
    return { x: 0, y: 0 };
  }
  const styles = window.getComputedStyle(el);
  const px = (value) => {
    const n = Number.parseFloat(value || "0");
    return Number.isFinite(n) ? n : 0;
  };
  const paddingX = px(styles.paddingLeft) + px(styles.paddingRight);
  const paddingY = px(styles.paddingTop) + px(styles.paddingBottom);
  const borderX = px(styles.borderLeftWidth) + px(styles.borderRightWidth);
  const borderY = px(styles.borderTopWidth) + px(styles.borderBottomWidth);
  return { x: paddingX + borderX, y: paddingY + borderY };
}

function getDefaultTermPadding() {
  if (termPadding) {
    return termPadding;
  }
  const probe = document.createElement("div");
  probe.className = "terminal-container";
  probe.style.position = "absolute";
  probe.style.visibility = "hidden";
  document.body.appendChild(probe);
  termPadding = getTermPaddingFor(probe);
  document.body.removeChild(probe);
  return termPadding;
}

function applyThemeTokens(tokens) {
  const root = document.documentElement.style;
  Object.entries(tokens).forEach(([key, value]) => {
    root.setProperty(`--${key.replace(/_/g, "-")}`, value);
  });
}

async function loadThemes() {
  const resp = await fetch("themes", { credentials: "include" });
  if (!resp.ok) {
    return;
  }
  const themes = await resp.json();
  themeSelect.innerHTML = "";
  for (const theme of themes) {
    const opt = document.createElement("option");
    opt.value = theme.name;
    opt.textContent = theme.label;
    themeSelect.appendChild(opt);
  }
  const saved = localStorage.getItem("bifrons.theme");
  if (saved) {
    themeSelect.value = saved;
  } else {
    const preferred = themes.find((t) => t.name === "solarized-nightfall");
    if (preferred) {
      themeSelect.value = preferred.name;
    }
  }
  const selected = themes.find((t) => t.name === themeSelect.value) || themes[0];
  if (selected) {
    applyThemeTokens(selected.tokens);
  }
  themeSelect.addEventListener("change", () => {
    const choice = themes.find((t) => t.name === themeSelect.value);
    if (choice) {
      applyThemeTokens(choice.tokens);
      localStorage.setItem("bifrons.theme", choice.name);
    }
  });
}

function setupTerminalForView(view) {
  if (!window.Terminal || !view || view.term) {
    return;
  }
  ensureViewDom(view);
  const term = new window.Terminal({
    cols: 80,
    rows: 24,
    fontFamily: '"JetBrains Mono", "SFMono-Regular", "Menlo", "Consolas", monospace',
    fontSize: 14,
    lineHeight: 1,
    theme: terminalTheme,
    cursorBlink: true,
    scrollback: 0,
    allowProposedApi: true,
  });
  let fitAddon = null;
  if (window.FitAddon && window.FitAddon.FitAddon) {
    fitAddon = new window.FitAddon.FitAddon();
    term.loadAddon(fitAddon);
  }
  term.open(view.termContainer);
  if (fitAddon) {
    fitAddon.fit();
  }
  if (!graphemeProviderSet && term.unicode && typeof term.unicode.getStringCellWidth === "function") {
    setGraphemeWidthProvider((text) => term.unicode.getStringCellWidth(text));
    graphemeProviderSet = true;
  }
  view.term = term;
  view.fitAddon = fitAddon;

  term.onData((data) => {
    if (!view) {
      return;
    }
    if (view.sessionId && activeSessionId !== view.sessionId) {
      setActiveSession(view.sessionId);
    }
    if (!data) {
      return;
    }
    if (view.ws && view.ws.readyState === WebSocket.OPEN) {
      view.ws.send(encodeFrameIn(data));
    } else {
      queueInput(view, data);
    }
  });

  const handleFocus = () => {
    if (view && view.sessionId && activeSessionId !== view.sessionId) {
      setActiveSession(view.sessionId);
    }
  };
  if (typeof term.onFocus === "function") {
    term.onFocus(handleFocus);
  } else if (term.textarea) {
    term.textarea.addEventListener("focus", handleFocus);
  } else if (term.element) {
    term.element.addEventListener("focusin", handleFocus);
  }
}

function handleScrollback(view, scrollback) {
  if (!view || !scrollback) {
    return;
  }
  if (!view.scrollbackRows) {
    view.scrollbackRows = [];
  }
  if (scrollback.clear) {
    view.scrollbackRows = [];
    view.scrollbackOffset = 0;
  }
  if (scrollback.cols) {
    if (view.scrollbackCols && view.scrollbackCols !== scrollback.cols) {
      view.scrollbackRows = [];
      view.scrollbackOffset = 0;
    }
    view.scrollbackCols = scrollback.cols;
  }
  if (scrollback.rows && scrollback.rows.length > 0) {
    for (const row of scrollback.rows) {
      view.scrollbackRows.push(row);
    }
  }
  if (view.scrollbackLimit > 0 && view.scrollbackRows.length > view.scrollbackLimit) {
    const extra = view.scrollbackRows.length - view.scrollbackLimit;
    view.scrollbackRows = view.scrollbackRows.slice(extra);
  }
  view.scrollbackOffset = clampScrollbackOffset(view, view.scrollbackOffset || 0);
  if (view === getActiveView()) {
    updateScrollbar(view);
    if (view.scrollbackOffset > 0) {
      renderView(view, true);
    }
  }
}

function handleFrame(view, frame) {
  if (!view || !frame || !frame.payload) return;
  const { type, data } = frame.payload;
  if (type === "sessions") {
    handleSessionsFrame(data);
    return;
  }
  if (type === "welcome") {
    markViewRecovered(view);
    if (data.serverCols && data.serverRows) {
      updateViewSize(view, data.serverCols, data.serverRows);
    }
    if (view.queuedInput && view.ws && view.ws.readyState === WebSocket.OPEN) {
      view.ws.send(encodeFrameIn(view.queuedInput));
      view.queuedInput = "";
    }
    return;
  }
  if (type === "snapshot") {
    if (!view.ready) {
      view.ready = true;
      scheduleBackoffReset(view);
    }
    clearLoadingBanner(view);
    markViewRecovered(view);
    view.snapshot = data;
    updateViewSize(view, data.cols, data.rows);
    renderView(view, true);
    if (view.queuedInput && view.ws && view.ws.readyState === WebSocket.OPEN) {
      view.ws.send(encodeFrameIn(view.queuedInput));
      view.queuedInput = "";
    }
    return;
  }
  if (type === "diff") {
    if (!view.snapshot) {
      return;
    }
    if (!view.ready) {
      view.ready = true;
      scheduleBackoffReset(view);
    }
    clearLoadingBanner(view);
    markViewRecovered(view);
    view.snapshot = applyDiffToSnapshot(view.snapshot, data);
    if (view.snapshot) {
      updateViewSize(view, view.snapshot.cols, view.snapshot.rows);
      renderView(view);
    }
    return;
  }
  if (type === "scrollback") {
    handleScrollback(view, data);
    return;
  }
  if (type === "wall") {
    // A single wall event is fanned out to all attached sessions.
    // Surface browser notifications for every tab that receives the wall.
    showWallNotification(data);
    return;
  }
  if (type === "error") {
    const msg = data.message || "connection error";
    const retryAfter = data.retryAfterSeconds || 0;
    if (msg.toLowerCase().includes("control not permitted")) {
      view.wantsControl = false;
      markViewRecovered(view);
      return;
    }
    if (msg.toLowerCase().includes("authorization")) {
      if (view.shareSession) {
        showLogin("share-unauthorized");
        return;
      }
      refreshSessionsGuarded("ws-authorization");
      return;
    }
    if (msg.toLowerCase().includes("no host connected")) {
      view.noHost = true;
      view.statusText = "";
      view.statusLevel = "error";
      clearLoadingBanner(view);
      clearConnectedBannerTimer(view);
      setViewConnectionState(view, "waiting");
      if (view === getActiveView()) {
        updateActiveStatus();
      }
      if (!view.shareSession) {
        // Force an immediate list refresh so newly created sessions can replace stale ones.
        void refreshSessionsGuarded("ws-no-host");
      }
      return;
    }
    scheduleReconnect(view, msg, retryAfter);
  }
}

async function ensureTerminalReady() {
  for (let i = 0; i < 20; i += 1) {
    if (window.Terminal && termGrid) {
      return;
    }
    await sleep(50);
  }
  throw new Error("xterm not ready");
}

function updateViewSize(view, cols, rows) {
  if (!view || !cols || !rows) {
    return;
  }
  const changed = view.serverCols !== cols || view.serverRows !== rows;
  view.serverCols = cols;
  view.serverRows = rows;
  view.scrollbackOffset = clampScrollbackOffset(view, view.scrollbackOffset || 0);
  if (changed) {
    view.pendingResize = true;
    view.lastRenderCols = 0;
    view.lastRenderRows = 0;
    if (view.visible) {
      scheduleLayout();
    }
  }
  if (view === getActiveView()) {
    updateScrollbar(view);
  }
}

function renderView(view, forceClear = false) {
  if (!view || !view.snapshot || !view.visible) {
    return;
  }
  if (view.pendingResize) {
    return;
  }
  if (!view.term) {
    setupTerminalForView(view);
  }
  if (!view.term) {
    return;
  }
  const effectiveSnapshot = view.scrollbackOffset > 0 ? buildScrollbackSnapshot(view) : view.snapshot;
  if (!effectiveSnapshot) {
    return;
  }
  const cols = effectiveSnapshot.cols;
  const rows = effectiveSnapshot.rows;
  const clear =
    forceClear ||
    view.lastRenderCols === 0 ||
    view.lastRenderRows === 0 ||
    view.lastRenderCols !== cols ||
    view.lastRenderRows !== rows;
  if (clear && view.term) {
    if (typeof view.term.reset === "function") {
      view.term.reset();
    }
    if (typeof view.term.clear === "function") {
      view.term.clear();
    }
  }
  const output = renderSnapshot(effectiveSnapshot, clear);
  view.term.write(output);
  view.lastRenderCols = cols;
  view.lastRenderRows = rows;
  view.noHost = false;
  if (view === getActiveView()) {
    setEmptyState(false);
    updateScrollbar(view);
  }
}

function clearLoadingBanner(view) {
  if (!view) {
    return;
  }
  view.loadingVisible = false;
  view.loadingText = "";
  if (view === getActiveView() && !view.statusText) {
    setStatusBanner("");
  }
}

function clearConnectedBannerTimer(view) {
  if (!view || !view.loadingTimer) {
    return;
  }
  clearTimeout(view.loadingTimer);
  view.loadingTimer = null;
}

function scheduleConnectedBannerExpiry(view) {
  if (!view) {
    return;
  }
  clearConnectedBannerTimer(view);
  view.loadingTimer = setTimeout(() => {
    view.loadingTimer = null;
    if (!view || view.ws == null || view.ready || view.connectionState === "offline" || view.connectionState === "waiting") {
      return;
    }
    if (view.statusText === "connected to relay") {
      view.statusText = "";
      view.statusLevel = "error";
    }
    if (view === getActiveView()) {
      updateActiveStatus();
    }
  }, 3000);
}

async function connectView(view) {
  try {
    if (!view) {
      return;
    }
    if (view.reconnectTimer) {
      if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
        window.__bifrons.debug.lastConnectStep = "blocked-reconnectTimer";
      }
      return;
    }
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastConnectView = {
        sessionId: view.sessionId || "",
        authPending: !!view.authPending,
        at: Date.now(),
      };
      window.__bifrons.debug.lastConnectPendingRaw = String(view.authPending);
      window.__bifrons.debug.lastConnectPendingType = typeof view.authPending;
    }
    if (view.authPending) {
      if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
        window.__bifrons.debug.lastConnectStep = "blocked-authPending";
      }
      return;
    }
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastConnectStep = "before-open";
    }
    openViewWS(view);
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastConnectAfterOpen = {
        sessionId: view.sessionId || "",
        at: Date.now(),
      };
      window.__bifrons.debug.lastConnectStep = "after-open";
    }
  } catch (err) {
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastConnectError = {
        message: err ? String(err) : "connectView error",
        at: Date.now(),
      };
    }
  }
}

function openViewWS(view) {
  if (!view) {
    return;
  }
  if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
    window.__bifrons.debug.lastOpenViewAttempt = {
      sessionId: view.sessionId || "",
      hadWS: !!view.ws,
      wsState: view.ws ? view.ws.readyState : null,
      at: Date.now(),
    };
  }
  if (view.ws && (view.ws.readyState === WebSocket.OPEN || view.ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  view.suppressReconnect = false;
  view.ready = false;
  stopReconnectTimers(view);
  setViewConnectionState(view, "connecting");
  const wsURL = new URL("ws/client", baseURL);
  wsURL.protocol = baseURL.protocol === "https:" ? "wss:" : "ws:";
  if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
    window.__bifrons.debug.lastWsUrl = wsURL.toString();
    window.__bifrons.debug.lastWsCreatedAt = Date.now();
    window.__bifrons.debug.lastWsOpenAt = 0;
    window.__bifrons.debug.lastWsClose = null;
    window.__bifrons.debug.lastWsErrorAt = 0;
  }
  const ws = new WebSocket(wsURL.toString());
  view.ws = ws;
  ws.binaryType = "arraybuffer";

  ws.onopen = () => {
    if (view.ws !== ws) {
      return;
    }
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastWsOpenAt = Date.now();
    }
    view.loadingVisible = true;
    view.loadingText = "loading from relay";
    view.statusText = "connected to relay";
    view.statusLevel = "info";
    if (view === getActiveView()) {
      updateActiveStatus();
    }
    scheduleConnectedBannerExpiry(view);
    const hello = encodeFrameHello({
      sessionId: view.shareSession ? "" : view.sessionId || "default",
      hello: {
        clientId: "",
        cols: view.serverCols || 80,
        rows: view.serverRows || 24,
        wantsControl: view.wantsControl !== false,
        lastSeq: 0,
        clientType: "web",
      },
    });
    ws.send(hello);
  };

  ws.onmessage = (event) => {
    if (view.ws !== ws) {
      return;
    }
    if (!(event.data instanceof ArrayBuffer)) {
      return;
    }
    try {
      const frame = decodeFrame(new Uint8Array(event.data));
      handleFrame(view, frame);
    } catch (err) {
      console.error("failed to decode frame", err);
    }
  };

  ws.onclose = (event) => {
    if (view.ws !== ws) {
      return;
    }
    view.ws = null;
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastWsClose = {
        code: event && event.code ? event.code : 0,
        reason: event && event.reason ? event.reason : "",
        wasClean: event ? !!event.wasClean : false,
        at: Date.now(),
      };
    }
    if (view.suppressReconnect) {
      view.suppressReconnect = false;
      return;
    }
    view.ready = false;
    clearLoadingBanner(view);
    setViewConnectionState(view, "offline");
    if (canReconnect(view)) {
      scheduleReconnect(view, "connection lost");
    }
  };

  ws.onerror = () => {
    if (view.ws !== ws) {
      return;
    }
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastWsErrorAt = Date.now();
    }
    clearLoadingBanner(view);
    setViewConnectionState(view, "offline");
  };
}

async function ensureAuthReady(reason) {
  if (authFailed) {
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastAuthReason = "auth_failed";
    }
    updateDebugState();
    return false;
  }
  if (lastSessionsError === "" && lastSessionsAt > 0 && Date.now() - lastSessionsAt < 10000) {
    return true;
  }
  if (authRefreshing) {
    for (let i = 0; i < 40; i += 1) {
      await new Promise((resolve) => setTimeout(resolve, 50));
      if (!authRefreshing) {
        break;
      }
    }
    return !authFailed;
  }
  authRefreshing = true;
  const ok = await attemptRefresh();
  authRefreshing = false;
  if (!ok) {
    authFailed = true;
    authFailureReason = reason || "unauthorized";
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastAuthReason = authFailureReason;
    }
    showLogin(authFailureReason);
    updateDebugState();
    return false;
  }
  authFailed = false;
  authFailureReason = "";
  updateDebugState();
  return true;
}

async function attemptRefresh() {
  try {
    const resp = await fetch("auth/refresh", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastAuthRefresh = {
        ok: resp.ok,
        status: resp.status,
        at: Date.now(),
      };
    }
    return resp.ok;
  } catch {
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastAuthRefresh = {
        ok: false,
        status: 0,
        at: Date.now(),
      };
    }
    return false;
  }
}

async function refreshSessions() {
  let resp;
  try {
    resp = await fetch("sessions", { credentials: "include" });
  } catch {
    sessionsAvailable = false;
    lastSessionsAt = Date.now();
    lastSessionsError = "offline";
    updateDebugState();
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastSessionsError = "offline";
      window.__bifrons.debug.lastSessionsAt = Date.now();
    }
    return "offline";
  }
  if (resp.status === 401) {
    lastSessionsAt = Date.now();
    lastSessionsError = "unauthorized";
    updateDebugState();
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastSessionsError = "unauthorized";
      window.__bifrons.debug.lastSessionsAt = Date.now();
    }
    return "unauthorized";
  }
  if (!resp.ok) {
    sessionsAvailable = false;
    lastSessionsAt = Date.now();
    lastSessionsError = `status_${resp.status}`;
    updateDebugState();
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastSessionsError = `status_${resp.status}`;
      window.__bifrons.debug.lastSessionsAt = Date.now();
    }
    return "offline";
  }
  const list = await resp.json();
  lastSessionsAt = Date.now();
  lastSessionsError = "";
  updateDebugState();
  if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
    window.__bifrons.debug.lastSessionsError = "";
    window.__bifrons.debug.lastSessionsCount = Array.isArray(list) ? list.length : 0;
    window.__bifrons.debug.lastSessionsAt = Date.now();
  }
  return applySessionsList(list);
}

function applySessionsList(list) {
  sessionsAvailable = true;
  sessions = Array.isArray(list) ? list : [];
  const activeIDs = new Set(sessions.map((s) => s.id));
  for (const [id, view] of views.entries()) {
    if (activeIDs.has(id)) {
      continue;
    }
    destroyView(view);
    views.delete(id);
  }
  const current = sessions.find((s) => s.id === activeSessionId);
  if (!current) {
    activeSessionId = pickSessionId(sessions);
  } else if (current.status !== "active") {
    const preferred = sessions.find((s) => s && s.id && s.status === "active");
    const currentView = views.get(activeSessionId);
    const currentUnavailable = !!(currentView && (currentView.noHost || currentView.connectionState === "waiting"));
    if (preferred && preferred.id && preferred.id !== activeSessionId && currentUnavailable) {
      activeSessionId = preferred.id;
    }
  }
  renderTabs();
  if (activeSessionId) {
    switchSession(activeSessionId);
    if (sessions.length === 1) {
      requestFocusActiveView();
    }
  } else {
    updateActiveStatus();
  }
  scheduleLayout();
  updateDebugState();
  return "ok";
}

function handleSessionsFrame(payload) {
  const list = [];
  if (payload && Array.isArray(payload.sessions)) {
    for (const session of payload.sessions) {
      if (!session) continue;
      list.push({
        id: session.id || "",
        name: session.name || "",
        status: session.status || "",
        last_active_at: session.lastActiveUnix
          ? new Date(session.lastActiveUnix * 1000).toISOString()
          : "",
      });
    }
  }
  applySessionsList(list);
}

async function refreshSessionsWithAuth() {
  let result = await refreshSessions();
  if (result === "unauthorized") {
    const refreshed = await attemptRefresh();
    if (refreshed) {
      result = await refreshSessions();
    }
  }
  window.__bifrons = window.__bifrons || {};
  window.__bifrons.authState = result;
  return result;
}

async function refreshSessionsGuarded(reason) {
  if (authRefreshing) {
    return "busy";
  }
  authRefreshing = true;
  if (reason) {
    authFailureReason = reason;
  }
  const result = await refreshSessionsWithAuth();
  authRefreshing = false;
  if (result === "unauthorized") {
    authFailed = true;
    disconnectAllViews();
    showLogin(authFailureReason || "unauthorized");
    return result;
  }
  authFailed = false;
  authFailureReason = "";
  showTerminal();
  return result;
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function scrollActiveTabIntoView() {
  if (!tabBar || !tabList || tabBar.classList.contains("hidden")) {
    return;
  }
  const activeBtn = tabList.querySelector(".tab-button.active");
  if (!activeBtn || typeof activeBtn.scrollIntoView !== "function") {
    return;
  }
  activeBtn.scrollIntoView({
    block: "nearest",
    inline: "nearest",
    behavior: "auto",
  });
}

function updateTabOverflowState() {
  if (!tabBar || !tabList) {
    return;
  }
  const hideFades = tabBar.classList.contains("hidden") || isFullscreenSingleMode();
  if (hideFades) {
    tabBar.classList.remove("has-overflow-left");
    tabBar.classList.remove("has-overflow-right");
    return;
  }
  const maxScroll = Math.max(0, tabList.scrollWidth - tabList.clientWidth);
  const left = tabList.scrollLeft;
  const epsilon = 1;
  const hasLeft = maxScroll > epsilon && left > epsilon;
  const hasRight = maxScroll > epsilon && left < maxScroll - epsilon;
  tabBar.classList.toggle("has-overflow-left", hasLeft);
  tabBar.classList.toggle("has-overflow-right", hasRight);
}

function renderTabs() {
  tabList.innerHTML = "";
  if (isFullscreenSingleMode()) {
    tabBar.classList.add("hidden");
    document.body.classList.remove("has-tabs");
    updateTabOverflowState();
    return;
  }
  if (!sessions || sessions.length === 0) {
    tabBar.classList.add("hidden");
    document.body.classList.remove("has-tabs");
    updateTabOverflowState();
    scheduleLayout();
    return;
  }
  sessions.forEach((session) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tab-button";
    const view = views.get(session.id);
    const label = session.name || session.id;
    if (view) {
      updateViewLabel(view);
    }
    button.textContent = label;
    if (session.id === activeSessionId) {
      button.classList.add("active");
    }
    button.addEventListener("click", () => {
      switchSession(session.id);
    });
    tabList.appendChild(button);
  });
  const showTabs = sessions.length > 1;
  tabBar.classList.toggle("hidden", !showTabs);
  document.body.classList.toggle("has-tabs", showTabs);
  requestAnimationFrame(() => {
    scrollActiveTabIntoView();
    updateTabOverflowState();
  });
  scheduleLayout();
}

function setActiveSession(sessionId) {
  if (!sessionId) {
    return;
  }
  if (activeSessionId === sessionId) {
    updateActiveStatus();
    return;
  }
  switchSession(sessionId);
}

function cycleActiveSession(direction) {
  if (!Array.isArray(sessions) || sessions.length === 0) {
    return;
  }
  const list = sessions.map((s) => s.id).filter((id) => id);
  if (list.length === 0) {
    return;
  }
  const currentIdx = list.indexOf(activeSessionId);
  const normalized = currentIdx === -1 ? 0 : currentIdx;
  const offset = direction < 0 ? -1 : 1;
  const nextIdx = (normalized + offset + list.length) % list.length;
  setActiveSession(list[nextIdx]);
}

function handleSessionShortcut(event) {
  if (!event || !event.ctrlKey) {
    return;
  }
  if (!event.altKey) {
    return;
  }
  const key = event.key || "";
  const code = event.code || "";
  const isTab = key === "Tab" || code === "Tab" || event.keyCode === 9;
  if (!isTab) {
    return;
  }
  if (terminalView.classList.contains("hidden")) {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  cycleActiveSession(event.shiftKey ? -1 : 1);
}

function switchSession(sessionId) {
  if (!sessionId) {
    return;
  }
  if (activeSessionId === sessionId) {
    updateActiveStatus();
    scheduleLayout();
    return;
  }
  activeSessionId = sessionId;
  const view = ensureView(sessionId);
  view.hiddenAt = 0;
  view.lastRenderCols = 0;
  view.lastRenderRows = 0;

  renderTabs();
  updateActiveStatus();
  requestFocusActiveView();
  connectView(view);
  if (view.snapshot) {
    updateViewSize(view, view.snapshot.cols, view.snapshot.rows);
    renderView(view, true);
  } else {
    updateViewSize(view, view.serverCols || defaultCols, view.serverRows || defaultRows);
    if (view === getActiveView()) {
      setEmptyState(true);
    }
  }
  updateScrollbar(view);
  scheduleLayout();
}

function hasShareSessionView() {
  for (const view of views.values()) {
    if (view && view.shareSession) {
      return true;
    }
  }
  return false;
}

function areAllActiveSessionsConnected() {
  if (!Array.isArray(sessions) || sessions.length === 0) {
    return true;
  }
  const activeSessions = sessions.filter((session) => session && session.id && session.status === "active");
  if (activeSessions.length === 0) {
    return true;
  }
  for (const session of activeSessions) {
    const view = views.get(session.id);
    if (!view) {
      return false;
    }
    if (view.connectionState !== "online" || !view.ready) {
      return false;
    }
    if (!view.ws || view.ws.readyState !== WebSocket.OPEN) {
      return false;
    }
  }
  return true;
}

function nextWallPollDelayMs() {
  const nowMs = Date.now();
  const allConnected = areAllActiveSessionsConnected();
  if (allConnected) {
    wallPollFastUntil = 0;
    return wallPollSlowInterval;
  }
  if (wallPollFastUntil === 0) {
    wallPollFastUntil = nowMs + wallPollFastWindow;
  }
  if (nowMs <= wallPollFastUntil) {
    return wallPollFastInterval;
  }
  return wallPollSlowInterval;
}

function scheduleWallPoll(delayMs) {
  if (wallPollTimer) {
    clearTimeout(wallPollTimer);
    wallPollTimer = null;
  }
  const generation = wallPollGeneration;
  wallPollTimer = setTimeout(() => {
    if (generation !== wallPollGeneration) {
      return;
    }
    void pollWallEvents(generation);
  }, Math.max(0, delayMs));
}

async function fetchWallEventsPage(sinceID, limit) {
  const params = new URLSearchParams();
  if (sinceID > 0) {
    params.set("since", `${sinceID}`);
  }
  if (limit > 0) {
    params.set("limit", `${limit}`);
  }
  const endpoint = params.toString() ? `wall/events?${params.toString()}` : "wall/events";
  let resp;
  try {
    resp = await fetch(endpoint, { credentials: "include" });
  } catch {
    return { status: "offline", payload: null };
  }
  if (resp.status === 401) {
    const refreshed = await attemptRefresh();
    if (!refreshed) {
      return { status: "unauthorized", payload: null };
    }
    try {
      resp = await fetch(endpoint, { credentials: "include" });
    } catch {
      return { status: "offline", payload: null };
    }
  }
  if (resp.status === 401) {
    return { status: "unauthorized", payload: null };
  }
  if (!resp.ok) {
    return { status: `status_${resp.status}`, payload: null };
  }
  try {
    const payload = await resp.json();
    return { status: "ok", payload };
  } catch {
    return { status: "invalid_json", payload: null };
  }
}

function handleWallPollUnauthorized() {
  authFailed = true;
  authFailureReason = "wall-unauthorized";
  disconnectAllViews();
  showLogin(authFailureReason);
}

async function pollWallEvents(generation) {
  if (generation !== wallPollGeneration) {
    return;
  }
  if (hasShareSessionView() || terminalView.classList.contains("hidden")) {
    return;
  }
  if (wallPollInFlight) {
    return;
  }
  wallPollInFlight = true;
  try {
    let since = wallPollCursor;
    for (let page = 0; page < 5; page += 1) {
      if (generation !== wallPollGeneration) {
        return;
      }
      const result = await fetchWallEventsPage(since, wallEventsPageLimit);
      if (generation !== wallPollGeneration) {
        return;
      }
      if (result.status === "unauthorized") {
        handleWallPollUnauthorized();
        return;
      }
      if (result.status !== "ok") {
        break;
      }
      const payload = result.payload || {};
      const events = Array.isArray(payload.events) ? payload.events : [];
      for (const event of events) {
        const timeoutSeconds = Number(event.timeout_seconds) || Number(event.timeoutSeconds) || 0;
        showWallNotification({
          sender: event.sender || "",
          source_session_name: event.session_name || "",
          message: event.message || "",
          timeoutSeconds: timeoutSeconds > 0 ? timeoutSeconds : 5,
          created_at: event.created_at || "",
        });
      }
      const nextID = Number(payload.next_id) || 0;
      if (nextID > since) {
        since = nextID;
      }
      if (!payload.has_more || events.length === 0) {
        break;
      }
    }
    if (since > wallPollCursor) {
      wallPollCursor = since;
    }
  } finally {
    wallPollInFlight = false;
    if (generation === wallPollGeneration && !terminalView.classList.contains("hidden")) {
      scheduleWallPoll(nextWallPollDelayMs());
    }
  }
}

function startWallEventPolling() {
  if (hasShareSessionView() || terminalView.classList.contains("hidden")) {
    return;
  }
  if (wallPollTimer || wallPollInFlight) {
    return;
  }
  scheduleWallPoll(0);
}

function stopWallEventPolling(resetState = false) {
  wallPollGeneration += 1;
  if (wallPollTimer) {
    clearTimeout(wallPollTimer);
    wallPollTimer = null;
  }
  wallPollInFlight = false;
  if (resetState) {
    wallPollCursor = 0;
    wallPollFastUntil = 0;
    recentWallNotifications.clear();
  }
}

function startSessionRefresh() {
  if (sessionRefreshTimer) {
    return;
  }
  refreshSessionsGuarded("sessions-refresh");
  sessionRefreshTimer = setInterval(() => {
    refreshSessionsGuarded("sessions-refresh");
  }, sessionRefreshInterval);
}

function stopSessionRefresh() {
  if (sessionRefreshTimer) {
    clearInterval(sessionRefreshTimer);
    sessionRefreshTimer = null;
  }
  stopWallEventPolling(true);
}

function startInactiveSweep() {
  if (inactiveSweepTimer) {
    return;
  }
  inactiveSweepTimer = setInterval(() => {
    const now = Date.now();
    for (const [id, view] of views.entries()) {
      if (view.visible || !view.hiddenAt) {
        continue;
      }
      if (now - view.hiddenAt < inactiveTTL) {
        continue;
      }
      destroyView(view);
      views.delete(id);
    }
  }, 1000);
}

function stopInactiveSweep() {
  if (!inactiveSweepTimer) {
    return;
  }
  clearInterval(inactiveSweepTimer);
  inactiveSweepTimer = null;
}

function pickSessionId(sessions) {
  if (!Array.isArray(sessions) || sessions.length === 0) {
    return "";
  }
  const active = sessions.find((s) => s.status === "active");
  if (active && active.id) {
    return active.id;
  }
  return sessions[0].id || "default";
}

function offsetFromThumbTop(top, maxTop, maxOffset) {
  if (maxOffset <= 0 || maxTop <= 0) {
    return 0;
  }
  const ratio = 1 - top / maxTop;
  return Math.round(ratio * maxOffset);
}

function handleScrollPointerDown(event) {
  if (!scrollTrack) return;
  const view = getActiveView();
  if (!view || !view.snapshot) return;
  const metrics = scrollMetrics(view);
  if (!metrics) return;
  event.preventDefault();
  scrollTrack.setPointerCapture(event.pointerId);
  scrollDrag.active = true;
  scrollDrag.pointerId = event.pointerId;
  scrollDrag.startY = event.clientY;
  scrollDrag.viewId = view.sessionId;

  let startTop = metrics.thumbTop;
  const clickY = event.clientY - metrics.trackTop;
  if (event.target !== scrollThumb) {
    const nextTop = Math.min(
      Math.max(0, clickY - metrics.thumbHeight / 2),
      metrics.maxTop,
    );
    const nextOffset = offsetFromThumbTop(nextTop, metrics.maxTop, metrics.maxOffset);
    setScrollbackOffset(view, nextOffset, true);
    startTop = nextTop;
  }
  scrollDrag.startTop = startTop;
}

function handleScrollPointerMove(event) {
  if (!scrollDrag.active) return;
  const view = views.get(scrollDrag.viewId);
  if (!view) return;
  const metrics = scrollMetrics(view);
  if (!metrics) return;
  const delta = event.clientY - scrollDrag.startY;
  const nextTop = Math.min(Math.max(0, scrollDrag.startTop + delta), metrics.maxTop);
  const nextOffset = offsetFromThumbTop(nextTop, metrics.maxTop, metrics.maxOffset);
  setScrollbackOffset(view, nextOffset);
}

function handleScrollPointerUp(event) {
  if (!scrollDrag.active) return;
  if (scrollTrack && scrollDrag.pointerId != null) {
    scrollTrack.releasePointerCapture(scrollDrag.pointerId);
  }
  scrollDrag.active = false;
  scrollDrag.pointerId = null;
  scrollDrag.viewId = "";
  scrollDrag.startTop = 0;
  scrollDrag.startY = 0;
}

function positionMenu() {
  const rect = menuToggle.getBoundingClientRect();
  const width = menu.offsetWidth || 240;
  const left = Math.min(Math.max(8, rect.left), window.innerWidth - width - 8);
  menu.style.left = `${left}px`;
  menu.style.top = `${rect.bottom + 8}px`;
  menu.style.right = "auto";
}

function openMenu() {
  menu.classList.remove("hidden");
  listIcon.classList.add("hidden");
  closeIcon.classList.remove("hidden");
  positionMenu();
}

function closeMenu() {
  menu.classList.add("hidden");
  listIcon.classList.remove("hidden");
  closeIcon.classList.add("hidden");
}

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  loginError.classList.add("hidden");
  const token = normalizedShareTokenInput();
  if (token !== "") {
    await attachWithShareToken(token);
    return;
  }
  const form = new FormData(loginForm);
  const payload = {
    username: form.get("username"),
    password: form.get("password"),
    totp: form.get("totp"),
    client_type: "web",
  };
  const resp = await fetch("auth/login", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!resp.ok) {
    loginError.textContent = "Login failed. Check your credentials.";
    loginError.classList.remove("hidden");
    return;
  }
  showTerminal();
  const result = await refreshSessionsGuarded("login");
  if (result === "unauthorized") {
    return;
  }
  startSessionRefresh();
  startInactiveSweep();
});

if (shareTokenInput) {
  shareTokenInput.addEventListener("input", () => {
    updateLoginModeForToken();
  });
}

menuToggle.addEventListener("click", () => {
  const open = !menu.classList.contains("hidden");
  if (open) {
    closeMenu();
  } else {
    openMenu();
  }
});

if (refreshSessionsButton) {
  refreshSessionsButton.addEventListener("click", async () => {
    if (refreshSessionsButton.disabled) {
      return;
    }
    const active = getActiveView();
    if (active && active.shareSession) {
      connectView(active);
      return;
    }
    if (typeof window !== "undefined" && window.__bifrons && window.__bifrons.debug) {
      window.__bifrons.debug.lastManualRefreshAt = Date.now();
    }
    refreshSessionsButton.disabled = true;
    try {
      await refreshSessionsGuarded("manual-refresh");
    } finally {
      refreshSessionsButton.disabled = false;
    }
  });
}

if (tabList) {
  tabList.addEventListener("scroll", () => {
    updateTabOverflowState();
  });
}

document.addEventListener("click", (event) => {
  if (menu.classList.contains("hidden")) {
    return;
  }
  const target = event.target;
  if (menu.contains(target) || menuToggle.contains(target)) {
    return;
  }
  closeMenu();
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") {
    return;
  }
  if (!menu.classList.contains("hidden")) {
    closeMenu();
  }
});

fontScaleInput.addEventListener("input", () => {
  const value = Number(fontScaleInput.value);
  fontScaleLabel.textContent = `${value}%`;
  fontState.scale = Math.max(0.5, Math.min(1.5, value / 100));
  localStorage.setItem("bifrons.fontScale", `${value}`);
  for (const view of views.values()) {
    if (view && view.term) {
      view.pendingResize = true;
    }
  }
  scheduleLayout();
});

if (fullscreenToggle) {
  fullscreenToggle.addEventListener("change", () => {
    fullscreenSingle = fullscreenToggle.checked;
    localStorage.setItem("bifrons.fullscreenSingle", fullscreenSingle ? "1" : "0");
    for (const view of views.values()) {
      if (view && view.term) {
        view.pendingResize = true;
      }
    }
    renderTabs();
    scheduleLayout();
  });
}

document.addEventListener(
  "fullscreenchange",
  () => {
    fullscreenActive = !!document.fullscreenElement;
    for (const view of views.values()) {
      if (view && view.term) {
        view.pendingResize = true;
      }
    }
    renderTabs();
    scheduleLayout();
  },
  true,
);

document.addEventListener(
  "keydown",
  (event) => {
    if (!event || event.key !== "F11") {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else {
      document.documentElement.requestFullscreen();
    }
  },
  true,
);

if (scrollTrack) {
  scrollTrack.addEventListener("pointerdown", handleScrollPointerDown);
  window.addEventListener("pointermove", handleScrollPointerMove);
  window.addEventListener("pointerup", handleScrollPointerUp);
  window.addEventListener("pointercancel", handleScrollPointerUp);
}

logoutButton.addEventListener("click", async () => {
  await fetch("auth/logout", {
    method: "POST",
    credentials: "include",
  });
  showLogin("logout");
});

document.addEventListener("keydown", handleSessionShortcut, true);

async function init() {
  setInitStep("loadThemes");
  await loadThemes();
  setInitStep("setupTerminal");
  await ensureTerminalReady();
  const resizeObserver = new ResizeObserver(() => {
    scheduleLayout();
  });
  resizeObserver.observe(terminalShell || termGrid);
  window.addEventListener("resize", scheduleLayout);
  const savedScale = Number(localStorage.getItem("bifrons.fontScale") || "100");
  fontScaleInput.value = `${savedScale}`;
  fontScaleLabel.textContent = `${savedScale}%`;
  fontState.scale = Math.max(0.5, Math.min(1.5, savedScale / 100));
  if (fullscreenToggle) {
    fullscreenSingle = localStorage.getItem("bifrons.fullscreenSingle") === "1";
    fullscreenToggle.checked = fullscreenSingle;
  }
  updateLoginModeForToken();
  enforceLoginInputTheme();
  if (bootstrapShareToken) {
    setInitStep("shareTokenBootstrap");
    const attached = await attachWithShareToken(bootstrapShareToken);
    if (attached) {
      setInitStep("ready");
      return;
    }
  }
  setInitStep("shareSession");
  const hasShareSession = await restoreShareSession();
  if (hasShareSession) {
    setInitStep("ready");
    return;
  }

  setInitStep("refreshSessions");
  let result = await refreshSessionsGuarded("init");
  if (result === "unauthorized") {
    setInitStep("refreshRetry");
    await sleep(200);
    result = await refreshSessionsGuarded("init-retry");
  }
  if (result === "ok" || result === "offline") {
    setInitStep("ready");
    startSessionRefresh();
    startInactiveSweep();
    return;
  }
  setInitStep("login");
}

init().catch((err) => {
  window.__bifrons = window.__bifrons || {};
  window.__bifrons.initError = err && err.message ? err.message : String(err);
  setInitStep("init-error");
  showLogin("init-error");
});

window.addEventListener("resize", () => {
  scheduleLayout();
  updateScrollbar(getActiveView());
  if (!menu.classList.contains("hidden")) {
    positionMenu();
  }
});

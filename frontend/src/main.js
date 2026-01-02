import {
  ListPorts,
  Flash,
  ChooseFile,
  MonitorPort,
  StopMonitor,
} from "../wailsjs/go/main/App.js";
import { EventsOn } from "../wailsjs/runtime/runtime.js";

const portSelect = document.getElementById("portSelect");
const baudSelect = document.getElementById("baudSelect");
const offsetSelect = document.getElementById("offsetSelect");
const customOffset = document.getElementById("customOffset");
const btnRefresh = document.getElementById("btnRefresh");
const btnChoose = document.getElementById("btnChoose");
const btnFlash = document.getElementById("btnFlash");
const btnMonitor = document.getElementById("btnMonitor");
const btnStopMonitor = document.getElementById("btnStopMonitor");
const btnClearLog = document.getElementById("btnClearLog");
const btnAutoScroll = document.getElementById("btnAutoScroll");
const filePath = document.getElementById("filePath");
const logArea = document.getElementById("log");
const progressContainer = document.getElementById("progressContainer");
const progressBar = document.getElementById("progressBar");
const progressText = document.getElementById("progressText");

// Show/hide custom offset field
offsetSelect.addEventListener("change", () => {
  if (offsetSelect.value === "custom") {
    customOffset.style.display = "block";
    customOffset.focus();
  } else {
    customOffset.style.display = "none";
  }
});

// Get current flash offset
function getFlashOffset() {
  if (offsetSelect.value === "custom") {
    const val = customOffset.value.trim();
    if (val.startsWith("0x") || val.startsWith("0X")) {
      return parseInt(val, 16);
    }
    return parseInt(val, 10);
  }
  return parseInt(offsetSelect.value, 16);
}

let isMonitoring = false;
let autoScrollEnabled = true;
let logLines = [];
const MAX_LOG_LINES = 300;

// Throttling for DOM updates
let pendingLines = [];
let updateScheduled = false;
let lastUpdateTime = 0;
const MIN_UPDATE_INTERVAL = 100; // Minimum 100ms between updates

// Schedule DOM update with throttling
function scheduleUpdate() {
  if (updateScheduled) return;

  const now = Date.now();
  const timeSinceLastUpdate = now - lastUpdateTime;

  if (timeSinceLastUpdate < MIN_UPDATE_INTERVAL) {
    // Delay update
    updateScheduled = true;
    setTimeout(() => {
      updateScheduled = false;
      flushPendingLines();
    }, MIN_UPDATE_INTERVAL - timeSinceLastUpdate);
  } else {
    flushPendingLines();
  }
}

// Apply pending lines to DOM
function flushPendingLines() {
  if (pendingLines.length === 0) return;

  lastUpdateTime = Date.now();

  // Add pending lines
  logLines.push(...pendingLines);
  pendingLines = [];

  // Limit number of lines
  if (logLines.length > MAX_LOG_LINES) {
    logLines = logLines.slice(-MAX_LOG_LINES);
  }

  // Update DOM once
  logArea.textContent = logLines.join("\n");

  // Auto-scroll
  if (autoScrollEnabled) {
    logArea.scrollTop = logArea.scrollHeight;
  }
}

// Format time with milliseconds
function formatTime(date) {
  const h = String(date.getHours()).padStart(2, "0");
  const m = String(date.getMinutes()).padStart(2, "0");
  const s = String(date.getSeconds()).padStart(2, "0");
  const ms = String(date.getMilliseconds()).padStart(3, "0");
  return `${h}:${m}:${s}.${ms}`;
}

// Add log entry
function log(msg) {
  const timestamp = formatTime(new Date());
  addLogLine(`[${timestamp}] ${msg}`);
}

// Efficient log line addition
function addLogLine(line) {
  pendingLines.push(line);
  scheduleUpdate();
}

// Update progress
function updateProgress(progress, message) {
  progressBar.style.width = `${progress}%`;
  progressText.textContent = `${progress}%`;
  if (message) {
    log(message);
  }
}

// Show/hide progress
function showProgress(show) {
  progressContainer.style.display = show ? "block" : "none";
  if (!show) {
    progressBar.style.width = "0%";
    progressText.textContent = "0%";
  }
}

// Setup progress events
EventsOn("flash-progress", (data) => {
  updateProgress(data.progress, data.message);
});

EventsOn("flash-log", (message) => {
  log(message);
});

// Port monitor events
EventsOn("monitor-data", (data) => {
  if (data.trim()) {
    const timestamp = formatTime(new Date());
    addLogLine(`[${timestamp}] ${data.trim()}`);
  }
});

EventsOn("monitor-error", (error) => {
  log(`❌ Monitor error: ${error}`);
  stopMonitoring();
});

EventsOn("monitor-stop", () => {
  stopMonitoring();
});

// Refresh and display ports
async function refreshPorts() {
  portSelect.innerHTML = "";
  try {
    const ports = await ListPorts();
    ports.forEach((p) => {
      const o = document.createElement("option");
      o.value = p;
      o.textContent = p;
      portSelect.appendChild(o);
    });
    log(`Found ${ports.length} ports`);
  } catch (e) {
    log("ListPorts error: " + e);
  }
}

// File selection
btnChoose.addEventListener("click", async () => {
  try {
    const res = await ChooseFile();
    if (res) {
      filePath.value = res;
      log("Selected " + res);
    }
  } catch (e) {
    log("File selection error: " + e);
  }
});

// Flash button
btnFlash.addEventListener("click", async () => {
  const port = portSelect.value;
  const file = filePath.value;
  const offset = getFlashOffset();

  if (!port || !file) {
    alert("Select port and file!");
    return;
  }

  if (isNaN(offset) || offset < 0) {
    alert("Invalid flash address!");
    return;
  }

  if (isMonitoring) {
    alert("Stop monitor before flashing!");
    return;
  }

  // Disable UI
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  btnMonitor.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
  offsetSelect.disabled = true;
  customOffset.disabled = true;

  // Clear log and show progress
  logArea.textContent = "";
  logLines = [];
  showProgress(true);

  log(
    `🚀 Starting flash ${file} → ${port} @ 0x${offset.toString(16).toUpperCase()}`
  );

  try {
    await Flash(port, file, offset);
    setTimeout(() => {
      alert("Flash completed successfully!");
    }, 100);
  } catch (e) {
    log("❌ Flash error: " + e);
    updateProgress(0, "Error");
    setTimeout(() => {
      alert("Flash error: " + e);
    }, 100);
  } finally {
    // Enable UI and hide progress
    setTimeout(() => {
      showProgress(false);
      btnFlash.disabled = false;
      btnChoose.disabled = false;
      btnRefresh.disabled = false;
      btnMonitor.disabled = false;
      portSelect.disabled = false;
      baudSelect.disabled = false;
      offsetSelect.disabled = false;
      customOffset.disabled = false;
    }, 1000); // Delay to show final state
  }
});

// Monitor button
btnMonitor.addEventListener("click", async () => {
  const port = portSelect.value;
  const baud = parseInt(baudSelect.value);
  if (!port) {
    alert("Select COM port for monitoring!");
    return;
  }

  try {
    // Clear log before starting monitor
    logArea.textContent = "";

    await MonitorPort(port, baud);
    startMonitoring();
    log(`🔍 Monitor started on ${port} (${baud} baud)`);
  } catch (e) {
    log("❌ Monitor start error: " + e);
    alert("Monitor start error: " + e);
  }
});

// Stop monitor button
btnStopMonitor.addEventListener("click", async () => {
  try {
    await StopMonitor();
    stopMonitoring();
  } catch (e) {
    log("❌ Monitor stop error: " + e);
  }
});

// Monitor functions
function startMonitoring() {
  isMonitoring = true;
  btnMonitor.style.display = "none";
  btnStopMonitor.style.display = "inline-block";
  btnFlash.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
}

function stopMonitoring() {
  isMonitoring = false;
  btnMonitor.style.display = "inline-block";
  btnStopMonitor.style.display = "none";
  btnFlash.disabled = false;
  portSelect.disabled = false;
  baudSelect.disabled = false;
}

// Clear log button
btnClearLog.addEventListener("click", () => {
  logLines = [];
  pendingLines = [];
  logArea.textContent = "";
  log("🗑️ Log cleared");
});

// Auto-scroll button
btnAutoScroll.addEventListener("click", () => {
  autoScrollEnabled = !autoScrollEnabled;

  if (autoScrollEnabled) {
    btnAutoScroll.classList.add("active");
    btnAutoScroll.textContent = "📜 Auto";
    requestAnimationFrame(() => {
      logArea.scrollTop = logArea.scrollHeight;
    });
  } else {
    btnAutoScroll.classList.remove("active");
    btnAutoScroll.textContent = "📜 Auto";
  }
});

// On start
btnRefresh.addEventListener("click", refreshPorts);

// Initialize auto-scroll button state
if (autoScrollEnabled) {
  btnAutoScroll.classList.add("active");
  btnAutoScroll.textContent = "📜 Auto";
} else {
  btnAutoScroll.classList.remove("active");
  btnAutoScroll.textContent = "📜 Auto";
}

refreshPorts();

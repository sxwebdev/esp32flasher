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

let isMonitoring = false;
let logUpdateTimeout = null; // Для батчинга обновлений лога
let autoScrollEnabled = true; // Автоскролл включен по умолчанию
let logLines = []; // Массив строк лога для эффективного управления
const MAX_LOG_LINES = 1000; // Увеличиваем лимит строк

// Залить лог
function log(msg) {
  const timestamp = new Date().toLocaleTimeString();
  addLogLine(`[${timestamp}] ${msg}`);
}

// Эффективное добавление строки в лог
function addLogLine(line) {
  logLines.push(line);

  // Ограничиваем количество строк (автоочистка как в терминале)
  if (logLines.length > MAX_LOG_LINES) {
    logLines = logLines.slice(-MAX_LOG_LINES);
  }

  // Сразу обновляем отображение
  logArea.textContent = logLines.join("\n");

  // Автоскролл если включен
  if (autoScrollEnabled) {
    logArea.scrollTop = logArea.scrollHeight;
  }
}

// Обновить прогресс
function updateProgress(progress, message) {
  progressBar.style.width = `${progress}%`;
  progressText.textContent = `${progress}%`;
  if (message) {
    log(message);
  }
}

// Показать/скрыть прогресс
function showProgress(show) {
  progressContainer.style.display = show ? "block" : "none";
  if (!show) {
    progressBar.style.width = "0%";
    progressText.textContent = "0%";
  }
}

// Настройка событий для прогресса
EventsOn("flash-progress", (data) => {
  updateProgress(data.progress, data.message);
});

EventsOn("flash-log", (message) => {
  log(message);
});

// События мониторинга порта
EventsOn("monitor-data", (data) => {
  // Данные уже приходят построчно, сразу отображаем
  if (data.trim()) {
    const timestamp = new Date().toLocaleTimeString();
    const logLine = `[${timestamp}] ${data.trim()}`;

    // Добавляем строку и сразу ограничиваем количество
    logLines.push(logLine);
    if (logLines.length > MAX_LOG_LINES) {
      logLines = logLines.slice(-MAX_LOG_LINES);
    }

    // Сразу обновляем отображение без батчинга
    logArea.textContent = logLines.join("\n");

    // Автоскролл если включен
    if (autoScrollEnabled) {
      logArea.scrollTop = logArea.scrollHeight;
    }
  }
});

EventsOn("monitor-error", (error) => {
  log(`❌ Ошибка мониторинга: ${error}`);
  stopMonitoring();
});

EventsOn("monitor-stop", () => {
  stopMonitoring();
});

// Получить и показать порты
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
    log(`Найдено портов: ${ports.length}`);
  } catch (e) {
    log("Ошибка ListPorts: " + e);
  }
}

// Выбор файла
btnChoose.addEventListener("click", async () => {
  try {
    const res = await ChooseFile();
    if (res) {
      filePath.value = res;
      log("Выбран " + res);
    }
  } catch (e) {
    log("Ошибка выбора файла: " + e);
  }
});

// Кнопка «Прошить»
btnFlash.addEventListener("click", async () => {
  const port = portSelect.value;
  const file = filePath.value;
  if (!port || !file) {
    alert("Укажите порт и файл!");
    return;
  }

  if (isMonitoring) {
    alert("Остановите мониторинг перед прошивкой!");
    return;
  }

  // Блокируем интерфейс
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  btnMonitor.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;

  // Очищаем лог и показываем прогресс
  logArea.textContent = "";
  showProgress(true);

  log(`🚀 Начинаем прошивку ${file} → ${port}`);

  try {
    await Flash(port, file);
    log("✅ Прошивка успешно завершена!");
    setTimeout(() => {
      alert("Прошивка завершена успешно!");
    }, 100);
  } catch (e) {
    log("❌ Ошибка прошивки: " + e);
    updateProgress(0, "Ошибка");
    setTimeout(() => {
      alert("Ошибка прошивки: " + e);
    }, 100);
  } finally {
    // Разблокируем интерфейс и скрываем прогресс
    setTimeout(() => {
      showProgress(false);
      btnFlash.disabled = false;
      btnChoose.disabled = false;
      btnRefresh.disabled = false;
      btnMonitor.disabled = false;
      portSelect.disabled = false;
      baudSelect.disabled = false;
    }, 1000); // Задержка, чтобы пользователь увидел финальное состояние
  }
});

// Кнопка мониторинга порта
btnMonitor.addEventListener("click", async () => {
  const port = portSelect.value;
  const baud = parseInt(baudSelect.value);
  if (!port) {
    alert("Выберите COM-порт для мониторинга!");
    return;
  }

  try {
    // Очищаем лог перед началом мониторинга
    logArea.textContent = "";

    await MonitorPort(port, baud);
    startMonitoring();
    log(`🔍 Мониторинг порта ${port} запущен (${baud} baud)`);
  } catch (e) {
    log("❌ Ошибка запуска мониторинга: " + e);
    alert("Ошибка запуска мониторинга: " + e);
  }
});

// Кнопка остановки мониторинга
btnStopMonitor.addEventListener("click", async () => {
  try {
    await StopMonitor();
    stopMonitoring();
  } catch (e) {
    log("❌ Ошибка остановки мониторинга: " + e);
  }
});

// Функции мониторинга
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

  // Очищаем таймер обновления лога если он есть
  if (logUpdateTimeout) {
    clearTimeout(logUpdateTimeout);
    logUpdateTimeout = null;
  }
}

// Кнопка очистки лога
btnClearLog.addEventListener("click", () => {
  logLines = []; // Очищаем массив строк
  logArea.textContent = "";
  log("🗑️ Лог очищен");
});

// Кнопка автоскролла
btnAutoScroll.addEventListener("click", () => {
  autoScrollEnabled = !autoScrollEnabled;

  if (autoScrollEnabled) {
    btnAutoScroll.classList.add("active");
    btnAutoScroll.textContent = "📜 Автоскролл";
    // Если включили автоскролл, сразу прокручиваем вниз
    requestAnimationFrame(() => {
      logArea.scrollTop = logArea.scrollHeight;
    });
    log("📜 Автоскролл включен");
  } else {
    btnAutoScroll.classList.remove("active");
    btnAutoScroll.textContent = "📜 Автоскролл";
    log("⏸️ Автоскролл отключен");
  }
});

// При старте
btnRefresh.addEventListener("click", refreshPorts);

// Инициализируем состояние кнопки автоскролла
if (autoScrollEnabled) {
  btnAutoScroll.classList.add("active");
  btnAutoScroll.textContent = "📜 Автоскролл";
} else {
  btnAutoScroll.classList.remove("active");
  btnAutoScroll.textContent = "📜 Автоскролл";
}

refreshPorts();

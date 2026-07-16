const messages = {
  en: {
    "app.title": "ESP32 Flasher",
    "brand.version": "VERSION",
    "brand.subtitle": "Firmware deployment console",
    "update.available": "Update available",
    "status.ready": "System ready",
    "status.flashing": "Flashing",
    "status.monitoring": "Monitoring",
    "status.updating": "Updating",
    "status.updateUnavailable": "Update unavailable",
    "status.updateFailed": "Update failed",
    "setup.configuration": "Configuration",
    "setup.device": "Device",
    "setup.serialPort": "Serial port",
    "setup.refreshPorts": "Refresh port list",
    "setup.selectPortHint": "Select a detected serial port.",
    "setup.detectedPort": "{port} — detected serial port",
    "setup.noPorts": "No serial ports detected.",
    "setup.portSpeed": "Port speed",
    "setup.baud": "BAUD",
    "setup.firmwareImage": "Firmware image",
    "setup.noFile": "No file selected",
    "setup.selectBin": "Select .bin",
    "setup.writeAddressHint": "The write address is detected automatically",
    "setup.flashFirmware": "Flash firmware",
    "keyboard.enter": "ENTER",
    "progress.writingFlash": "Writing Flash",
    "progress.starting": "Starting flash…",
    "progress.firmwareLoaded": "Firmware loaded",
    "progress.connecting": "Connecting to ESP32…",
    "progress.synchronizing": "Synchronizing…",
    "progress.detectingChip": "Detecting chip…",
    "progress.attachingFlash": "Attaching SPI Flash…",
    "progress.erasingFlash": "Erasing Flash…",
    "progress.transferringData": "Transferring data…",
    "progress.writing": "Writing {percent}% ({speed} KiB/s)",
    "progress.verifyingMD5": "Verifying MD5…",
    "progress.finishing": "Finishing…",
    "progress.done": "Done!",
    "progress.complete": "Flashing complete!",
    "progress.failed": "Flash failed",
    "terminal.deviceOutput": "Device output",
    "terminal.serialConsole": "Serial console",
    "terminal.monitor": "Monitor",
    "terminal.stop": "Stop",
    "terminal.autoScroll": "Auto-scroll",
    "terminal.autoScrollTitle": "Automatic scrolling",
    "terminal.clearConsole": "Clear console",
    "terminal.copyLog": "Copy console output",
    "terminal.copied": "Copied",
    "terminal.copiedAria": "Console output copied",
    "terminal.binaryGuard": "UTF-8 / BINARY GUARD",
    "logCounter.one": "{count} line",
    "logCounter.few": "{count} lines",
    "logCounter.many": "{count} lines",
    "logCounter.other": "{count} lines",
    "log.truncated": "truncated",
    "log.monitorError": "❌ Monitor error: {error}",
    "log.updateCheckUnavailable": "Update check unavailable: {error}",
    "log.versionUnavailable": "Could not read app version: {error}",
    "log.updateRequiresInstalledApp": "Automatic updates require the installed application in a writable location.",
    "log.updateFailed": "Update failed: {error}",
    "log.portsFound": "Found {count} ports",
    "log.listPortsError": "ListPorts error: {error}",
    "log.selectedFile": "Selected {file}",
    "log.fileSelectionError": "File selection error: {error}",
    "log.startFlash": "🚀 Starting flash {file} → {port}",
    "log.flashSuccess": "✅ Firmware flashed successfully!",
    "log.flashError": "❌ Flash error: {error}",
    "log.monitoring": "🔍 Monitoring {port} at {baud} baud",
    "log.monitorStartError": "❌ Could not start monitoring: {error}",
    "log.monitorStopError": "❌ Could not stop monitoring: {error}",
    "log.cleared": "🗑️ Log cleared",
    "log.copyError": "❌ Could not copy console output: {error}",
    "log.autoScrollEnabled": "📜 Auto-scroll enabled",
    "log.autoScrollDisabled": "⏸️ Auto-scroll disabled",
    "update.version": "Update {version}",
    "update.requiresInstalledApp": "Automatic updates require the installed application",
    "update.availableAria": "Update available; install the application to update automatically",
    "update.unavailable": "Automatic update unavailable",
    "update.writableLocation": "The application must be installed in a writable location",
    "update.downloading": "Downloading update…",
    "update.retry": "Update failed — retry",
    "alert.stopBeforeUpdate": "Stop monitoring or wait for flashing to finish before updating.",
    "alert.selectPortAndFile": "Select a port and firmware file.",
    "alert.stopMonitorBeforeFlash": "Stop serial monitoring before flashing.",
    "alert.flashSuccess": "Firmware flashed successfully!",
    "alert.flashError": "Flash error: {error}",
    "alert.selectPortToMonitor": "Select a serial port to monitor.",
    "alert.monitorStartError": "Could not start monitoring: {error}",
  },
  ru: {
    "app.title": "Прошивальщик ESP32",
    "brand.version": "ВЕРСИЯ",
    "brand.subtitle": "Консоль загрузки прошивки",
    "update.available": "Доступно обновление",
    "status.ready": "Система готова",
    "status.flashing": "Прошивка",
    "status.monitoring": "Мониторинг",
    "status.updating": "Обновление",
    "status.updateUnavailable": "Обновление недоступно",
    "status.updateFailed": "Ошибка обновления",
    "setup.configuration": "Настройка",
    "setup.device": "Устройство",
    "setup.serialPort": "Последовательный порт",
    "setup.refreshPorts": "Обновить список портов",
    "setup.selectPortHint": "Выберите обнаруженный последовательный порт.",
    "setup.detectedPort": "{port} — обнаруженный последовательный порт",
    "setup.noPorts": "Последовательные порты не обнаружены.",
    "setup.portSpeed": "Скорость порта",
    "setup.baud": "БОД",
    "setup.firmwareImage": "Образ прошивки",
    "setup.noFile": "Файл не выбран",
    "setup.selectBin": "Выбрать .bin",
    "setup.writeAddressHint": "Адрес записи определяется автоматически",
    "setup.flashFirmware": "Прошить устройство",
    "keyboard.enter": "ВВОД",
    "progress.writingFlash": "Запись Flash",
    "progress.starting": "Начало прошивки…",
    "progress.firmwareLoaded": "Файл прошивки загружен",
    "progress.connecting": "Подключение к ESP32…",
    "progress.synchronizing": "Синхронизация…",
    "progress.detectingChip": "Определение чипа…",
    "progress.attachingFlash": "Подключение SPI Flash…",
    "progress.erasingFlash": "Очистка Flash…",
    "progress.transferringData": "Передача данных…",
    "progress.writing": "Запись {percent}% ({speed} КиБ/с)",
    "progress.verifyingMD5": "Проверка MD5…",
    "progress.finishing": "Завершение…",
    "progress.done": "Готово!",
    "progress.complete": "Прошивка завершена!",
    "progress.failed": "Ошибка прошивки",
    "terminal.deviceOutput": "Вывод устройства",
    "terminal.serialConsole": "Последовательная консоль",
    "terminal.monitor": "Монитор",
    "terminal.stop": "Стоп",
    "terminal.autoScroll": "Автопрокрутка",
    "terminal.autoScrollTitle": "Автоматическая прокрутка",
    "terminal.clearConsole": "Очистить консоль",
    "terminal.copyLog": "Копировать вывод консоли",
    "terminal.copied": "Скопировано",
    "terminal.copiedAria": "Вывод консоли скопирован",
    "terminal.binaryGuard": "UTF-8 / ЗАЩИТА ОТ ДВОИЧНЫХ ДАННЫХ",
    "logCounter.one": "{count} строка",
    "logCounter.few": "{count} строки",
    "logCounter.many": "{count} строк",
    "logCounter.other": "{count} строки",
    "log.truncated": "сокращено",
    "log.monitorError": "❌ Ошибка монитора: {error}",
    "log.updateCheckUnavailable": "Не удалось проверить обновления: {error}",
    "log.versionUnavailable": "Не удалось определить версию приложения: {error}",
    "log.updateRequiresInstalledApp": "Автоматическое обновление доступно только для установленного приложения в доступном для записи каталоге.",
    "log.updateFailed": "Ошибка обновления: {error}",
    "log.portsFound": "Найдено портов: {count}",
    "log.listPortsError": "Ошибка получения списка портов: {error}",
    "log.selectedFile": "Выбран файл: {file}",
    "log.fileSelectionError": "Ошибка выбора файла: {error}",
    "log.startFlash": "🚀 Начало прошивки: {file} → {port}",
    "log.flashSuccess": "✅ Прошивка успешно записана!",
    "log.flashError": "❌ Ошибка прошивки: {error}",
    "log.monitoring": "🔍 Мониторинг {port} на скорости {baud} бод",
    "log.monitorStartError": "❌ Не удалось запустить мониторинг: {error}",
    "log.monitorStopError": "❌ Не удалось остановить мониторинг: {error}",
    "log.cleared": "🗑️ Консоль очищена",
    "log.copyError": "❌ Не удалось скопировать вывод консоли: {error}",
    "log.autoScrollEnabled": "📜 Автопрокрутка включена",
    "log.autoScrollDisabled": "⏸️ Автопрокрутка выключена",
    "update.version": "Обновить до {version}",
    "update.requiresInstalledApp": "Автоматическое обновление доступно только для установленного приложения",
    "update.availableAria": "Доступно обновление; установите приложение для автоматического обновления",
    "update.unavailable": "Автоматическое обновление недоступно",
    "update.writableLocation": "Приложение должно быть установлено в доступном для записи каталоге",
    "update.downloading": "Загрузка обновления…",
    "update.retry": "Ошибка обновления — повторить",
    "alert.stopBeforeUpdate": "Остановите мониторинг или дождитесь окончания прошивки перед обновлением.",
    "alert.selectPortAndFile": "Выберите порт и файл прошивки.",
    "alert.stopMonitorBeforeFlash": "Остановите мониторинг порта перед прошивкой.",
    "alert.flashSuccess": "Прошивка успешно записана!",
    "alert.flashError": "Ошибка прошивки: {error}",
    "alert.selectPortToMonitor": "Выберите последовательный порт для мониторинга.",
    "alert.monitorStartError": "Не удалось запустить мониторинг: {error}",
  },
};

const fallbackLanguage = "en";

// The WebView exposes the OS preferred UI language through navigator.language.
// Only the primary preference is used: a Russian secondary language must not
// unexpectedly switch an otherwise English system to Russian.
export function detectLanguage(preferredLanguage = globalThis.navigator?.language) {
  return String(preferredLanguage || "").toLowerCase().startsWith("ru") ? "ru" : fallbackLanguage;
}

export const language = detectLanguage();

function assertTranslationCoverage() {
  const sourceKeys = Object.keys(messages.en).sort();
  for (const [locale, dictionary] of Object.entries(messages)) {
    const localeKeys = Object.keys(dictionary).sort();
    if (sourceKeys.length !== localeKeys.length || sourceKeys.some((key, index) => key !== localeKeys[index])) {
      throw new Error(`Translation keys for ${locale} must match the English dictionary`);
    }
  }
}

assertTranslationCoverage();

export function t(key, values = {}) {
  const template = messages[language][key] ?? messages[fallbackLanguage][key];
  if (template === undefined) {
    throw new Error(`Missing translation: ${key}`);
  }
  return template.replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? `{${name}}`));
}

export function formatLogCounter(count) {
  const plural = new Intl.PluralRules(language).select(count);
  const key = `logCounter.${plural}`;
  return t(messages[language][key] ? key : "logCounter.other", { count });
}

export function formatTime(date) {
  return date.toLocaleTimeString(language);
}

export function translateDocument() {
  document.documentElement.lang = language;
  document.title = t("app.title");

  for (const element of document.querySelectorAll("[data-i18n]")) {
    element.textContent = t(element.dataset.i18n);
  }
  for (const element of document.querySelectorAll("[data-i18n-placeholder]")) {
    element.placeholder = t(element.dataset.i18nPlaceholder);
  }
  for (const element of document.querySelectorAll("[data-i18n-title]")) {
    element.title = t(element.dataset.i18nTitle);
  }
  for (const element of document.querySelectorAll("[data-i18n-aria-label]")) {
    element.setAttribute("aria-label", t(element.dataset.i18nAriaLabel));
  }
}

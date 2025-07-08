package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	serialport "go.bug.st/serial"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ListPorts возвращает список COM-портов
func (a *App) ListPorts() ([]string, error) {
	return serialport.GetPortsList()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ChooseFile открывает диалог выбора файла
func (a *App) ChooseFile() (string, error) {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите файл прошивки",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Firmware Files",
				Pattern:     "*.bin",
			},
		},
	})

	return filePath, err
}

// emitProgress отправляет прогресс в frontend
func (a *App) emitProgress(progress int, message string) {
	runtime.EventsEmit(a.ctx, "flash-progress", map[string]interface{}{
		"progress": progress,
		"message":  message,
	})
}

// emitLog отправляет лог сообщение в frontend
func (a *App) emitLog(message string) {
	runtime.EventsEmit(a.ctx, "flash-log", message)
}

// Flash прошивает только application.bin на адрес 0x10000 используя встроенную реализацию esptool
func (a *App) Flash(portName, filePath string) error {
	// Проверить что файл существует
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}

	a.emitProgress(0, "Начинаем прошивку...")
	a.emitLog("🔄 Инициализация...")

	// Считать файл
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	a.emitProgress(10, "Файл загружен")
	a.emitLog(fmt.Sprintf("📄 Загружен файл: %d байт", len(data)))

	// Создать ESP32 флешер
	flasher, err := NewESP32FlasherWithProgress(portName, a)
	if err != nil {
		return fmt.Errorf("failed to create flasher: %w", err)
	}
	defer flasher.Close()

	a.emitProgress(20, "Подключение к ESP32...")
	a.emitLog("🔗 Подключение к ESP32...")

	// Прошить данные с прогрессом
	if err := flasher.FlashData(data, 0x10000); err != nil {
		a.emitProgress(0, "Ошибка прошивки")
		return fmt.Errorf("failed to flash: %w", err)
	}

	a.emitProgress(100, "Прошивка завершена!")
	a.emitLog("✅ Прошивка успешно завершена!")

	return nil
}

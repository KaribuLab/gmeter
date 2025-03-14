package logger

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/rs/zerolog"
)

// Logger es un adaptador que implementa una interfaz similar a logrus
// pero utiliza zerolog internamente para mejor rendimiento y soporte de colores
type Logger struct {
	zlog        zerolog.Logger
	consoleOut  zerolog.ConsoleWriter
	fileWriter  *lumberjack.Logger
	multiWriter io.Writer
}

// LogConfig contiene la configuración para el logger
type LogConfig struct {
	// Configuración del archivo de log
	Filename   string // Ruta del archivo de log
	MaxSize    int    // Tamaño máximo en MB antes de rotar
	MaxBackups int    // Número máximo de archivos de respaldo
	MaxAge     int    // Días máximos para mantener los archivos
	Compress   bool   // Comprimir los archivos rotados

	// Configuración general
	Level      string // Nivel de log (trace, debug, info, warn, error, fatal, panic)
	UseConsole bool   // Usar salida a consola
}

// DefaultLogConfig devuelve una configuración por defecto
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Filename:   "gmeter.log",
		MaxSize:    10,   // 10 MB
		MaxBackups: 5,    // 5 archivos de respaldo
		MaxAge:     30,   // 30 días
		Compress:   true, // Comprimir los archivos rotados
		Level:      "info",
		UseConsole: true,
	}
}

// New crea un nuevo logger con salida a la consola con colores
func New() *Logger {
	return NewWithConfig(DefaultLogConfig())
}

// NewWithConfig crea un nuevo logger con la configuración especificada
func NewWithConfig(config LogConfig) *Logger {
	// Configurar el nivel global de log
	setLogLevel(config.Level)

	// Configurar la salida de consola con colores
	consoleOut := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		NoColor:    false, // Habilitar colores
	}

	// Personalizar los colores y formato
	consoleOut.FormatLevel = func(i interface{}) string {
		level := fmt.Sprintf("%s", i)
		switch level {
		case "trace":
			return "\x1b[37mTRACE\x1b[0m"
		case "debug":
			return "\x1b[36mDEBUG\x1b[0m"
		case "info":
			return "\x1b[32mINFO \x1b[0m"
		case "warn":
			return "\x1b[33mWARN \x1b[0m"
		case "error":
			return "\x1b[31mERROR\x1b[0m"
		case "fatal":
			return "\x1b[35mFATAL\x1b[0m"
		case "panic":
			return "\x1b[41mPANIC\x1b[0m"
		default:
			return level
		}
	}

	// Personalizar el formato del mensaje
	consoleOut.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("\x1b[1m%s\x1b[0m", i) // Mensaje en negrita
	}

	// Personalizar el formato del timestamp
	consoleOut.FormatTimestamp = func(i interface{}) string {
		t := fmt.Sprintf("%s", i)
		return fmt.Sprintf("\x1b[90m%s\x1b[0m", t) // Timestamp en gris
	}

	// Personalizar el formato de los campos
	consoleOut.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("\x1b[34m%s=\x1b[0m", i) // Nombre del campo en azul
	}

	consoleOut.FormatFieldValue = func(i interface{}) string {
		return fmt.Sprintf("\x1b[36m%s\x1b[0m", i) // Valor del campo en cyan
	}

	// Crear el logger
	var writer io.Writer
	var fileWriter *lumberjack.Logger

	if config.UseConsole {
		writer = consoleOut
	} else {
		writer = io.Discard
	}

	// Crear el logger
	zlog := zerolog.New(writer).With().Timestamp().Logger()

	return &Logger{
		zlog:       zlog,
		consoleOut: consoleOut,
		fileWriter: fileWriter,
	}
}

// setLogLevel configura el nivel de log global
func setLogLevel(level string) {
	switch level {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// SetOutput establece la salida del logger
func (l *Logger) SetOutput(w io.Writer) {
	// Si es un archivo, usar formato JSON sin colores
	l.zlog = zerolog.New(w).With().Timestamp().Logger()
}

// SetLogFile configura el archivo de log con rotación
func (l *Logger) SetLogFile(filename string, useConsole bool) {
	l.SetLogFileWithConfig(LogConfig{
		Filename:   filename,
		MaxSize:    10,   // 10 MB
		MaxBackups: 5,    // 5 archivos de respaldo
		MaxAge:     30,   // 30 días
		Compress:   true, // Comprimir los archivos rotados
		UseConsole: useConsole,
	})
}

// SetLogFileWithConfig configura el archivo de log con rotación usando una configuración personalizada
func (l *Logger) SetLogFileWithConfig(config LogConfig) {
	// Configurar el rotador de logs
	l.fileWriter = &lumberjack.Logger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,    // megabytes
		MaxBackups: config.MaxBackups, // número de archivos
		MaxAge:     config.MaxAge,     // días
		Compress:   config.Compress,   // comprimir los archivos rotados
	}

	// Crear un multi-writer si se usa la consola
	if config.UseConsole {
		l.multiWriter = io.MultiWriter(l.consoleOut, l.fileWriter)
	} else {
		l.multiWriter = l.fileWriter
	}

	// Actualizar el logger
	l.zlog = zerolog.New(l.multiWriter).With().Timestamp().Logger()
}

// SetFormatter es un método de compatibilidad con logrus (no hace nada en zerolog)
func (l *Logger) SetFormatter(formatter interface{}) {
	// No hace nada, solo para compatibilidad con logrus
}

// WithField añade un campo al logger
func (l *Logger) WithField(key string, value interface{}) *Entry {
	return &Entry{l.zlog.With().Interface(key, value).Logger()}
}

// WithFields añade múltiples campos al logger
func (l *Logger) WithFields(fields map[string]interface{}) *Entry {
	ctx := l.zlog.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &Entry{ctx.Logger()}
}

// WithError añade un error al logger
func (l *Logger) WithError(err error) *Entry {
	return &Entry{l.zlog.With().Err(err).Logger()}
}

// Trace registra un mensaje con nivel TRACE
func (l *Logger) Trace(args ...interface{}) {
	l.zlog.Trace().Msg(fmt.Sprint(args...))
}

// Debug registra un mensaje con nivel DEBUG
func (l *Logger) Debug(args ...interface{}) {
	l.zlog.Debug().Msg(fmt.Sprint(args...))
}

// Info registra un mensaje con nivel INFO
func (l *Logger) Info(args ...interface{}) {
	l.zlog.Info().Msg(fmt.Sprint(args...))
}

// Warn registra un mensaje con nivel WARN
func (l *Logger) Warn(args ...interface{}) {
	l.zlog.Warn().Msg(fmt.Sprint(args...))
}

// Error registra un mensaje con nivel ERROR
func (l *Logger) Error(args ...interface{}) {
	l.zlog.Error().Msg(fmt.Sprint(args...))
}

// Fatal registra un mensaje con nivel FATAL y termina la aplicación
func (l *Logger) Fatal(args ...interface{}) {
	l.zlog.Fatal().Msg(fmt.Sprint(args...))
}

// Panic registra un mensaje con nivel PANIC y causa un panic
func (l *Logger) Panic(args ...interface{}) {
	l.zlog.Panic().Msg(fmt.Sprint(args...))
}

// Tracef registra un mensaje formateado con nivel TRACE
func (l *Logger) Tracef(format string, args ...interface{}) {
	l.zlog.Trace().Msg(fmt.Sprintf(format, args...))
}

// Debugf registra un mensaje formateado con nivel DEBUG
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.zlog.Debug().Msg(fmt.Sprintf(format, args...))
}

// Infof registra un mensaje formateado con nivel INFO
func (l *Logger) Infof(format string, args ...interface{}) {
	l.zlog.Info().Msg(fmt.Sprintf(format, args...))
}

// Warnf registra un mensaje formateado con nivel WARN
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.zlog.Warn().Msg(fmt.Sprintf(format, args...))
}

// Errorf registra un mensaje formateado con nivel ERROR
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.zlog.Error().Msg(fmt.Sprintf(format, args...))
}

// Fatalf registra un mensaje formateado con nivel FATAL y termina la aplicación
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.zlog.Fatal().Msg(fmt.Sprintf(format, args...))
}

// Panicf registra un mensaje formateado con nivel PANIC y causa un panic
func (l *Logger) Panicf(format string, args ...interface{}) {
	l.zlog.Panic().Msg(fmt.Sprintf(format, args...))
}

// Entry es un adaptador para entradas de log con campos
type Entry struct {
	zlog zerolog.Logger
}

// Trace registra un mensaje con nivel TRACE
func (e *Entry) Trace(args ...interface{}) {
	e.zlog.Trace().Msg(fmt.Sprint(args...))
}

// Debug registra un mensaje con nivel DEBUG
func (e *Entry) Debug(args ...interface{}) {
	e.zlog.Debug().Msg(fmt.Sprint(args...))
}

// Info registra un mensaje con nivel INFO
func (e *Entry) Info(args ...interface{}) {
	e.zlog.Info().Msg(fmt.Sprint(args...))
}

// Warn registra un mensaje con nivel WARN
func (e *Entry) Warn(args ...interface{}) {
	e.zlog.Warn().Msg(fmt.Sprint(args...))
}

// Error registra un mensaje con nivel ERROR
func (e *Entry) Error(args ...interface{}) {
	e.zlog.Error().Msg(fmt.Sprint(args...))
}

// Fatal registra un mensaje con nivel FATAL y termina la aplicación
func (e *Entry) Fatal(args ...interface{}) {
	e.zlog.Fatal().Msg(fmt.Sprint(args...))
}

// Panic registra un mensaje con nivel PANIC y causa un panic
func (e *Entry) Panic(args ...interface{}) {
	e.zlog.Panic().Msg(fmt.Sprint(args...))
}

// Tracef registra un mensaje formateado con nivel TRACE
func (e *Entry) Tracef(format string, args ...interface{}) {
	e.zlog.Trace().Msg(fmt.Sprintf(format, args...))
}

// Debugf registra un mensaje formateado con nivel DEBUG
func (e *Entry) Debugf(format string, args ...interface{}) {
	e.zlog.Debug().Msg(fmt.Sprintf(format, args...))
}

// Infof registra un mensaje formateado con nivel INFO
func (e *Entry) Infof(format string, args ...interface{}) {
	e.zlog.Info().Msg(fmt.Sprintf(format, args...))
}

// Warnf registra un mensaje formateado con nivel WARN
func (e *Entry) Warnf(format string, args ...interface{}) {
	e.zlog.Warn().Msg(fmt.Sprintf(format, args...))
}

// Errorf registra un mensaje formateado con nivel ERROR
func (e *Entry) Errorf(format string, args ...interface{}) {
	e.zlog.Error().Msg(fmt.Sprintf(format, args...))
}

// Fatalf registra un mensaje formateado con nivel FATAL y termina la aplicación
func (e *Entry) Fatalf(format string, args ...interface{}) {
	e.zlog.Fatal().Msg(fmt.Sprintf(format, args...))
}

// Panicf registra un mensaje formateado con nivel PANIC y causa un panic
func (e *Entry) Panicf(format string, args ...interface{}) {
	e.zlog.Panic().Msg(fmt.Sprintf(format, args...))
}

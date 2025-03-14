package main

import (
	"fmt"
	"os"

	"github.com/KaribuLab/gmeter/internal/config"
	"github.com/KaribuLab/gmeter/internal/logger"
	"github.com/KaribuLab/gmeter/internal/runner"
	"github.com/spf13/cobra"
)

var (
	cfgFile       string
	log           *logger.Logger
	verbose       bool
	logLevel      string
	logMaxSize    int
	logMaxBackups int
	logMaxAge     int
	logCompress   bool
)

func init() {
	log = logger.New()
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "gmeter",
		Short: "GMeter - Una herramienta para pruebas de stress HTTP",
		Long: `GMeter es una herramienta para realizar pruebas de stress HTTP.
Permite configurar múltiples servicios, hilos por segundo, y generar reportes detallados.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mostrar un mensaje de inicio
			log.Info("Iniciando GMeter...")

			// Cargar la configuración
			log.Info("Cargando configuración...")
			cfg, err := config.LoadConfig(cfgFile)
			if err != nil {
				return fmt.Errorf("error al cargar la configuración: %w", err)
			}
			log.Info("Configuración cargada correctamente")

			// Configurar el logger para archivo si es necesario
			if cfg.LogFile != "" {
				log.Infof("Configurando archivo de log: %s", cfg.LogFile)

				// Configurar el logger con rotación
				logConfig := logger.LogConfig{
					Filename:   cfg.LogFile,
					MaxSize:    logMaxSize,
					MaxBackups: logMaxBackups,
					MaxAge:     logMaxAge,
					Compress:   logCompress,
					Level:      logLevel,
					UseConsole: verbose,
				}

				log.SetLogFileWithConfig(logConfig)
				log.Info("Sistema de logs configurado con rotación de archivos")
			}

			// Iniciar el runner con la configuración cargada
			log.Info("Iniciando pruebas de stress...")
			r := runner.NewRunner(cfg, log)
			return r.Run()
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "archivo de configuración (por defecto es ./gmeter.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", true, "mostrar logs en consola")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "nivel de log (trace, debug, info, warn, error, fatal, panic)")
	rootCmd.PersistentFlags().IntVar(&logMaxSize, "log-max-size", 10, "tamaño máximo del archivo de log en MB antes de rotar")
	rootCmd.PersistentFlags().IntVar(&logMaxBackups, "log-max-backups", 5, "número máximo de archivos de respaldo")
	rootCmd.PersistentFlags().IntVar(&logMaxAge, "log-max-age", 30, "días máximos para mantener los archivos de log")
	rootCmd.PersistentFlags().BoolVar(&logCompress, "log-compress", true, "comprimir los archivos de log rotados")

	if err := rootCmd.Execute(); err != nil {
		log.WithError(err).Fatal("Error al ejecutar el comando")
		os.Exit(1)
	}
}

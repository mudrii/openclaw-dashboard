package dashboard

import appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"

type BotConfig = appconfig.BotConfig
type ThemeConfig = appconfig.ThemeConfig
type RefreshConfig = appconfig.RefreshConfig
type ServerConfig = appconfig.ServerConfig
type AIConfig = appconfig.AIConfig
type LogsConfig = appconfig.LogsConfig
type AlertsConfig = appconfig.AlertsConfig
type MetricThreshold = appconfig.MetricThreshold
type SystemConfig = appconfig.SystemConfig
type Config = appconfig.Config

func defaultConfig() Config {
	return appconfig.Default()
}

func loadConfig(dir string) Config {
	return appconfig.Load(dir)
}

func readDotenv(path string) map[string]string {
	return appconfig.ReadDotenv(path)
}

func expandHome(path string) string {
	return appconfig.ExpandHome(path)
}

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DatabaseType represents the type of database
type DatabaseType string

const (
	DatabaseTypeSQLite DatabaseType = "sqlite"
	DatabaseTypeMySQL  DatabaseType = "mysql"
)

// DefaultKFactor is the default Elo K-factor used when not configured.
const DefaultKFactor = 32

// Config holds all configuration for the application
type Config struct {
	Database DatabaseConfig `yaml:"database"`
	Server   ServerConfig   `yaml:"server"`
	Cmd      CmdConfig      `yaml:"cmd"`
	Services ServicesConfig `yaml:"services"`
	Judge    JudgeConfig    `yaml:"judge"`
	Rating   RatingConfig   `yaml:"rating"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type   DatabaseType `yaml:"type"`
	SQLite SQLiteConfig `yaml:"sqlite"`
	MySQL  MySQLConfig  `yaml:"mysql"`
}

// SQLiteConfig holds SQLite-specific configuration
type SQLiteConfig struct {
	Path string `yaml:"path"`
}

// MySQLConfig holds MySQL-specific configuration
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port int `yaml:"port"`
}

// CmdConfig holds command service configuration
type CmdConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// ServicesConfig holds service enable/disable flags
type ServicesConfig struct {
	SQL   bool `yaml:"sql"`
	Judge bool `yaml:"judge"`
	File  bool `yaml:"file"`
}

// JudgeConfig holds distributed-judge configuration
type JudgeConfig struct {
	Mode              string `yaml:"mode"`               // local | coordinator | worker
	CoordinatorAddr   string `yaml:"coordinator_addr"`   // e.g. http://coordinator:9091
	CoordinatorPort   int    `yaml:"coordinator_port"`   // port the coordinator listens on
	WorkerConcurrency int    `yaml:"worker_concurrency"` // max concurrent judgments per worker
	PerTaskConcurrency int    `yaml:"per_task_concurrency"` // max test cases judged in parallel within one submission
	LocalJudge        bool   `yaml:"local_judge"`        // coordinator also judges locally
	AuthToken         string `yaml:"auth_token"`         // shared secret required by coordinator (empty = no auth)
}

// RatingConfig holds tunable rating-calculation parameters
type RatingConfig struct {
	KFactor    int     `yaml:"k_factor"`    // Elo K-factor (rating volatility)
	RankWeight float64 `yaml:"rank_weight"` // weight of rank-based performance vs score-based (0..1)
}

// GlobalConfig is the global configuration instance
var GlobalConfig *Config

// configFilePath stores the path the config was loaded from, so it can be saved.
var configFilePath string

// Load reads and parses the configuration file
func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	GlobalConfig = &cfg
	configFilePath = path
	return nil
}

// Save writes the current GlobalConfig back to the loaded config file. This is
// used by admin endpoints that customize runtime settings (e.g. rating weights).
func Save() error {
	if configFilePath == "" {
		return fmt.Errorf("config file path unknown")
	}
	data, err := yaml.Marshal(GlobalConfig)
	if err != nil {
		return err
	}
	return os.WriteFile(configFilePath, data, 0644)
}

// GetDatabaseType returns the database type from config
func GetDatabaseType() DatabaseType {
	if GlobalConfig == nil {
		return DatabaseTypeSQLite
	}
	return GlobalConfig.Database.Type
}

// GetSQLitePath returns the SQLite database path from config
func GetSQLitePath() string {
	if GlobalConfig == nil || GlobalConfig.Database.SQLite.Path == "" {
		return "data/app.db"
	}
	return GlobalConfig.Database.SQLite.Path
}

// GetMySQLConfig returns the MySQL configuration from config
func GetMySQLConfig() MySQLConfig {
	if GlobalConfig == nil {
		return MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			User:     "root",
			Password: "",
			DBName:   "gooj",
		}
	}
	return GlobalConfig.Database.MySQL
}

// GetServerPort returns the server port from config
func GetServerPort() int {
	if GlobalConfig == nil {
		return 8081
	}
	return GlobalConfig.Server.Port
}

// GetCmdHost returns the command service host from config
func GetCmdHost() string {
	if GlobalConfig == nil {
		return "127.0.0.1"
	}
	return GlobalConfig.Cmd.Host
}

// GetCmdPort returns the command service port from config
func GetCmdPort() int {
	if GlobalConfig == nil {
		return 9090
	}
	return GlobalConfig.Cmd.Port
}

// GetCmdAddr returns the full command service address
func GetCmdAddr() string {
	return fmt.Sprintf("%s:%d", GetCmdHost(), GetCmdPort())
}

// GetServiceEnabled returns whether a specific service is enabled
func GetServiceEnabled(service string) bool {
	if GlobalConfig == nil {
		return true // default to enabled
	}

	switch service {
	case "sql":
		return GlobalConfig.Services.SQL
	case "judge":
		return GlobalConfig.Services.Judge
	case "file":
		return GlobalConfig.Services.File
	default:
		return true
	}
}

// IsSQLEnabled returns whether SQL service is enabled
func IsSQLEnabled() bool {
	return GetServiceEnabled("sql")
}

// IsJudgeEnabled returns whether judge service is enabled
func IsJudgeEnabled() bool {
	return GetServiceEnabled("judge")
}

// IsFileEnabled returns whether file service is enabled
func IsFileEnabled() bool {
	return GetServiceEnabled("file")
}

// GetJudgeMode returns the distributed-judge mode (local, coordinator, worker).
func GetJudgeMode() string {
	if GlobalConfig == nil || GlobalConfig.Judge.Mode == "" {
		return "local"
	}
	return GlobalConfig.Judge.Mode
}

// GetCoordinatorAddr returns the coordinator HTTP address for workers.
func GetCoordinatorAddr() string {
	if GlobalConfig == nil || GlobalConfig.Judge.CoordinatorAddr == "" {
		return "http://localhost:9091"
	}
	return GlobalConfig.Judge.CoordinatorAddr
}

// GetCoordinatorPort returns the port the coordinator listens on.
func GetCoordinatorPort() int {
	if GlobalConfig == nil || GlobalConfig.Judge.CoordinatorPort == 0 {
		return 9091
	}
	return GlobalConfig.Judge.CoordinatorPort
}

// GetWorkerConcurrency returns max concurrent judgments per worker.
func GetWorkerConcurrency() int {
	if GlobalConfig == nil || GlobalConfig.Judge.WorkerConcurrency == 0 {
		return 4
	}
	return GlobalConfig.Judge.WorkerConcurrency
}

// GetPerTaskConcurrency returns how many test cases of a single submission may be
// judged in parallel (single-task multi-threaded evaluation). A non-positive or
// unset value defaults to 8 so a large submission cannot spawn unbounded Docker
// containers and exhaust host memory.
func GetPerTaskConcurrency() int {
	if GlobalConfig == nil || GlobalConfig.Judge.PerTaskConcurrency <= 0 {
		return 8
	}
	return GlobalConfig.Judge.PerTaskConcurrency
}

// GetCoordinatorLocalJudge reports whether the coordinator also judges locally.
func GetCoordinatorLocalJudge() bool {
	if GlobalConfig == nil {
		return true
	}
	return GlobalConfig.Judge.LocalJudge
}

// GetJudgeAuthToken returns the shared secret required by the coordinator.
// An empty token means authentication is disabled (suitable for trusted LANs).
func GetJudgeAuthToken() string {
	if GlobalConfig == nil {
		return ""
	}
	return GlobalConfig.Judge.AuthToken
}

// SetRatingConfig updates the rating weights at runtime and persists them.
func SetRatingConfig(kFactor int, rankWeight float64) error {
	if GlobalConfig == nil {
		return fmt.Errorf("config not loaded")
	}
	if kFactor <= 0 {
		kFactor = DefaultKFactor
	}
	if rankWeight < 0 {
		rankWeight = 0
	}
	if rankWeight > 1 {
		rankWeight = 1
	}
	GlobalConfig.Rating.KFactor = kFactor
	GlobalConfig.Rating.RankWeight = rankWeight
	return Save()
}

// GetRatingKFactor returns the configured Elo K-factor (default 32).
func GetRatingKFactor() int {
	if GlobalConfig == nil || GlobalConfig.Rating.KFactor <= 0 {
		return DefaultKFactor
	}
	return GlobalConfig.Rating.KFactor
}

// GetRatingRankWeight returns the configured rank-vs-score weight (default 1.0).
func GetRatingRankWeight() float64 {
	if GlobalConfig == nil {
		return 1.0
	}
	w := GlobalConfig.Rating.RankWeight
	if w < 0 {
		w = 0
	}
	if w > 1 {
		w = 1
	}
	return w
}

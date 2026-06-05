package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/config"
	"github.com/sig-cloudnative/nuts/pkg/datasource"
	"github.com/sig-cloudnative/nuts/pkg/db"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
	"github.com/sig-cloudnative/nuts/pkg/eventlog"
	"github.com/sig-cloudnative/nuts/pkg/log"
	"github.com/sig-cloudnative/nuts/pkg/metrics"
	"github.com/sig-cloudnative/nuts/pkg/policy"
	"github.com/sig-cloudnative/nuts/pkg/task"
	"github.com/sig-cloudnative/nuts/pkg/trace"
	"go.opentelemetry.io/otel/attribute"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/time/rate"
)

// Core 核心应用结构
type Core struct {
	// 配置
	Config config.ConfigManager

	// 日志
	Logger log.Logger

	// EventBus
	EventBus eventbus.EventBus

	// 数据源管理器
	DataSourceManager *datasource.DataSourceManager

	// 策略引擎
	PolicyEngine policy.PolicyEngine

	// 任务存储
	taskStore task.TaskStore
	// 归档清理器
	archiveCleaner *task.ArchiveCleaner
	// 状态机引擎
	stateMachineEngine task.StateMachineEngine
	// 超时检查器
	timeoutChecker *task.TimeoutChecker

	// HTTP服务器
	HTTPServer *http.Server
	serverAddr string

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 事件速率限制
	eventLimiter *rate.Limiter

	// 数据源事件 channel
	eventCh chan *common.Event

	// policy.matched 直接 channel（替代 EventBus 订阅）
	policyMatchedCh chan *common.Event

	// 最大并发任务信号量
	taskSem chan struct{}

	// 度量收集
	Metrics          common.MetricsRecorder
	prometheusEnabled bool

	// 分布式追踪
	tracerProvider interface{ Shutdown(context.Context) error }
	tracer         oteltrace.Tracer

	// 事件日志
	eventLog eventlog.EventLog

	// 双启动/双停止防护
	started atomic.Bool
	stopped atomic.Bool
}

// Config 核心配置
type Config struct {
	// 配置文件路径
	ConfigFile string

	// Server配置
	Server ServerConfig

	// 数据源配置
	DataSources []datasource.DataSourceConfig

	// 策略引擎配置
	Policy PolicyConfig

	// 日志配置
	Log LogConfig
}

// ServerConfig 服务器配置
type ServerConfig struct {
	// 服务器地址，支持 tcp://host:port 或 unix:///path/to/socket 格式
	Address string `toml:"address"`
}

// PolicyConfig 策略引擎配置
type PolicyConfig struct {
	Type string `toml:"type"`
}

// LogConfig 日志配置
type LogConfig struct {
	OutputPath string `toml:"output_path"`
	Encoding   string `toml:"encoding"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		ConfigFile: "configs/nuts.toml",
		Server: ServerConfig{
			Address: "tcp://0.0.0.0:8080",
		},
		Policy: PolicyConfig{
			Type: "cel",
		},
		Log: LogConfig{
			OutputPath: "",
			Encoding:   "console",
		},
	}
}

// New 创建Core实例
func New(cfg *Config) (*Core, error) {
	ctx, cancel := context.WithCancel(context.Background())

	core := &Core{
		ctx:    ctx,
		cancel: cancel,
	}

	// 1. 初始化日志
	if err := core.initLogger(); err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	// 2. 初始化配置
	if err := core.initConfig(cfg.ConfigFile); err != nil {
		return nil, fmt.Errorf("init config: %w", err)
	}

	// 初始化 policyMatchedCh（配置加载后才可读取配置值）
	channelSize := core.Config.GetInt("scheduler.policy_matched_buffer_size")
	if channelSize <= 0 {
		channelSize = 100
	}
	core.policyMatchedCh = make(chan *common.Event, channelSize)

	// 初始化 ID 生成器（根据 [id] 配置选择 UUID 或 Snowflake）
	core.initIDGenerator()

	// 根据配置重新配置日志
	if err := core.reconfigureLogger(); err != nil {
		return nil, fmt.Errorf("reconfigure logger: %w", err)
	}

	// 初始化指标收集
	core.initMetrics()

	// 初始化分布式追踪
	core.initTracer()

	// 3. 初始化EventBus
	if err := core.initEventBus(); err != nil {
		return nil, fmt.Errorf("init eventbus: %w", err)
	}

	// 4. 初始化数据源
	if err := core.initDataSources(); err != nil {
		return nil, fmt.Errorf("init datasources: %w", err)
	}

	// 5. 初始化事件日志
	core.initEventLog()

	// 6. 初始化任务调度
	if err := core.initTaskScheduler(); err != nil {
		return nil, fmt.Errorf("init task scheduler: %w", err)
	}

	// 6. 初始化策略引擎
	if err := core.initPolicyEngine(); err != nil {
		return nil, fmt.Errorf("init policy engine: %w", err)
	}

	// 7. 初始化回滚管理

	core.Logger.Info("Core initialized successfully")
	return core, nil
}

// initLogger 初始化日志
func (c *Core) initLogger() error {
	// 使用默认配置初始化日志
	logger, err := log.NewZapLoggerWithConfig(log.ZapConfig{
		Level:      "info",
		Encoding:   "console",
		OutputPath: "",
	})
	if err != nil {
		return err
	}

	// 设置为默认 logger
	log.SetDefault(logger)

	c.Logger = logger
	return nil
}

// reconfigureLogger 从配置重新配置日志
func (c *Core) reconfigureLogger() error {
	if c.Config == nil {
		return nil
	}

	// 从配置读取日志设置
	logLevel := c.Config.GetString("global.log_level")
	if logLevel == "" {
		logLevel = "info"
	}

	logEncoding := c.Config.GetString("log.encoding")
	if logEncoding == "" {
		logEncoding = "console"
	}

	logOutputPath := c.Config.GetString("log.output_path")

	// 采样配置
	samplingEnabled := c.Config.GetBool("log.sampling_enabled")
	samplingInitial := c.Config.GetInt("log.sampling_initial")
	samplingThereafter := c.Config.GetInt("log.sampling_thereafter")
	samplingTickMillis := c.Config.GetInt("log.sampling_tick_millis")

	logger, err := log.NewZapLoggerWithConfig(log.ZapConfig{
		Level:              logLevel,
		Encoding:           logEncoding,
		OutputPath:         logOutputPath,
		SamplingEnabled:    samplingEnabled,
		SamplingInitial:    samplingInitial,
		SamplingThereafter: samplingThereafter,
		SamplingTickMillis: int64(samplingTickMillis),
	})
	if err != nil {
		return err
	}

	// 设置为默认 logger
	log.SetDefault(logger)

	c.Logger = logger
	return nil
}

// initIDGenerator 初始化ID生成器（根据 [id] 配置选择 UUID 或 Snowflake）
func (c *Core) initIDGenerator() {
	idCfg := c.Config.GetMap("id")
	generator, err := common.IDGeneratorFactory.Create(idCfg)
	if err != nil {
		c.Logger.Warn("ID generator init failed, using default UUID",
			log.String("error", err.Error()),
		)
		return
	}
	common.SetDefaultGenerator(generator)
	idType, _ := idCfg["type"].(string)
	if idType == "" {
		idType = "uuid"
	}
	c.Logger.Info("ID generator initialized", log.String("type", idType))
}

// initMetrics 初始化指标收集
func (c *Core) initMetrics() {
	metricsType := c.Config.GetString("metrics.type")
	switch metricsType {
	case "prometheus":
		namespace := c.Config.GetString("metrics.namespace")
		if namespace == "" {
			namespace = "nuts"
		}
		c.Metrics = metrics.NewPrometheusMetrics(namespace)
		c.prometheusEnabled = true
		c.Logger.Info("Prometheus metrics enabled", log.String("namespace", namespace))
	default:
		c.Metrics = common.NoopMetricsRecorder{}
	}

	// 包裹告警装饰器
	if c.Config.GetBool("metrics.alert.enabled") {
		alertCfg := metrics.DefaultAlertConfig()
		if v := c.Config.Get("metrics.alert.timeout_rate_threshold"); v != nil {
			if f, ok := v.(float64); ok && f > 0 {
				alertCfg.TimeoutRateThreshold = f
			}
		}
		if v := c.Config.Get("metrics.alert.error_rate_threshold"); v != nil {
			if f, ok := v.(float64); ok && f > 0 {
				alertCfg.ErrorRateThreshold = f
			}
		}
		if v := c.Config.GetInt("metrics.alert.window_size_seconds"); v > 0 {
			alertCfg.WindowSize = time.Duration(v) * time.Second
		}
		if v := c.Config.GetInt("metrics.alert.check_interval_seconds"); v > 0 {
			alertCfg.CheckInterval = time.Duration(v) * time.Second
		}
		c.Metrics = metrics.NewAlertMetrics(c.Metrics, c.Logger, alertCfg)
		c.Logger.Info("Metrics alerting enabled")
	}
}

// initTracer 初始化分布式追踪
func (c *Core) initTracer() {
	if !c.Config.GetBool("tracing.enabled") {
		return
	}

	endpoint := c.Config.GetString("tracing.endpoint")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	serviceName := c.Config.GetString("tracing.service_name")
	if serviceName == "" {
		serviceName = "nuts"
	}

	sampleRate := 1.0
	if v := c.Config.Get("tracing.sample_rate"); v != nil {
		if f, ok := v.(float64); ok {
			sampleRate = f
		}
	}

	tp, err := trace.InitTracer(c.ctx, trace.Config{
		Enabled:     true,
		Endpoint:    endpoint,
		ServiceName: serviceName,
		SampleRate:  sampleRate,
	})
	if err != nil {
		c.Logger.Error("Failed to init tracer", log.Error(err))
		return
	}

	c.tracerProvider = tp
	c.tracer = trace.GetTracer("nuts-core")
	c.Logger.Info("Tracing enabled", log.String("endpoint", endpoint))
}

// initConfig 初始化配置
func (c *Core) initConfig(configFile string) error {
	// 创建TOML配置管理器
	tomlManager := config.NewTOMLConfigManager()

	// 加载配置文件
	if configFile != "" {
		if err := tomlManager.Load(configFile); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				c.Logger.Warn("Config file not found, using defaults", log.String("file", configFile))
			} else {
				return fmt.Errorf("load config file %s: %w", configFile, err)
			}
		} else {
			c.Logger.Info("Config file loaded", log.String("file", configFile))
		}
	}

	c.Config = tomlManager

	return nil
}

// initEventBus 初始化EventBus
func (c *Core) initEventBus() error {
	eventBusType := c.Config.GetString("eventbus.type")
	if eventBusType == "" {
		c.Logger.Info("No eventbus type configured, using NoopEventBus (standalone mode)")
		c.EventBus = eventbus.NewNoopEventBus()
		return nil
	}

	c.Logger.Info("Using eventbus type from config", log.String("type", eventBusType))

	// 从配置读取具体配置
	eventBusConfig := c.Config.GetMap(fmt.Sprintf("eventbus.%s", eventBusType))
	if eventBusConfig == nil {
		eventBusConfig = make(map[string]interface{})
	}

	// 注入 type
	eventBusConfig["type"] = eventBusType

	// 使用工厂创建 EventBus（未注册的类型会返回错误）
	eventBus, err := eventbus.Factory.CreateWithMap(eventBusConfig)
	if err != nil {
		return fmt.Errorf("create eventbus %s: %w", eventBusType, err)
	}

	c.Logger.Info("EventBus created", log.String("type", eventBusType))

	// 设置 logger
	if grpcEventBus, ok := eventBus.(*eventbus.GRPCEventBus); ok {
		grpcEventBus.SetLogger(c.Logger)
	}

	c.EventBus = eventBus
	return nil
}

// initEventLog 初始化事件日志
func (c *Core) initEventLog() {
	logType := c.Config.GetString("eventlog.type")
	if logType == "" || logType == "disabled" {
		c.Logger.Info("EventLog disabled")
		return
	}

	switch logType {
	case "memory":
		size := c.Config.GetInt("eventlog.buffer_size")
		if size <= 0 {
			size = 10000
		}
		c.eventLog = eventlog.NewAsyncEventLog(eventlog.NewRingBufferEventLog(size), 4096)
		c.Logger.Info("EventLog initialized (memory)", log.Int("buffer_size", size))
	case "db":
		dbType := c.Config.GetString("eventlog.db.type")
		if dbType == "" {
			dbType = "sqlite"
		}
		dbPath := c.Config.GetString("eventlog.db.path")
		if dbPath == "" {
			dbPath = "data/eventlog.db"
		}
		database, err := db.DefaultFactory.Create(db.Config{Type: dbType, Path: dbPath})
		if err != nil {
			c.Logger.Warn("Failed to create EventLog DB, falling back to memory", log.Error(err))
			c.eventLog = eventlog.NewAsyncEventLog(eventlog.NewRingBufferEventLog(10000), 4096)
			return
		}
		c.eventLog = eventlog.NewAsyncEventLog(eventlog.NewDBEventLog(database), 4096)
		c.Logger.Info("EventLog initialized (db)", log.String("path", dbPath))
	default:
		c.Logger.Warn("Unknown eventlog type, disabling", log.String("type", logType))
	}

	// 启动清理器
	retentionHours := c.Config.GetInt("eventlog.retention_hours")
	if retentionHours > 0 {
		cleanupInterval := c.Config.GetString("eventlog.cleanup_interval")
		interval, err := time.ParseDuration(cleanupInterval)
		if err != nil {
			interval = time.Hour
		}
		cleaner := eventlog.NewCleaner(c.eventLog, interval, time.Duration(retentionHours)*time.Hour)
		cleaner.Start()
		c.Logger.Info("EventLog cleaner started",
			log.Int("retention_hours", retentionHours),
			log.String("interval", interval.String()))
	}
}

// initTaskScheduler 初始化任务调度器（单存储模式）
func (c *Core) initTaskScheduler() error {
	// 加载状态机配置
	smConfig, err := task.LoadStateMachineConfig(c.Config)
	if err != nil {
		return fmt.Errorf("load state machine config: %w", err)
	}

	// 校验状态机配置合法性
	if err := task.ValidateStateMachineConfig(smConfig); err != nil {
		return fmt.Errorf("invalid state machine config: %w", err)
	}

	// 创建单存储（统一使用 engineTaskStore + ArchivedAt 模式）
	dbType := c.Config.GetString("task.db.type")
	if dbType == "" {
		dbType = "memory"
	}

	database, err := db.DefaultFactory.Create(db.Config{
		Type:      dbType,
		Path:      c.Config.GetString("task.db.path"),
		TableName: "tasks",
	})
	if err != nil {
		return fmt.Errorf("create task db: %w", err)
	}
	c.taskStore = task.NewTaskStore(database, 100)
	c.Logger.Info("Task store created", log.String("type", dbType), log.String("impl", "engineTaskStore"))

	// 状态机引擎使用单一存储
	c.stateMachineEngine = task.NewDefaultStateMachineEngine(c.taskStore, smConfig, c.EventBus)
	c.stateMachineEngine.SetMetrics(c.Metrics)
	if c.eventLog != nil {
		if sme, ok := c.stateMachineEngine.(interface{ SetEventLog(eventlog.EventLog) }); ok {
			sme.SetEventLog(c.eventLog)
		}
	}

	// 归档自动清理（基于 ArchivedAt）
	c.archiveCleaner, err = c.initArchiveCleaner()
	if err != nil {
		return fmt.Errorf("init archive cleaner: %w", err)
	}

	// 创建超时检查器
	timeoutCheckInterval := 30 * time.Second
	if intervalStr := c.Config.GetString("task.scheduler.timeout_check_interval"); intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil && d > 0 {
			timeoutCheckInterval = d
		}
	}
	rebuildInterval := c.Config.GetInt("task.scheduler.timeout_rebuild_interval")
	if rebuildInterval <= 0 {
		rebuildInterval = 5
	}
	c.timeoutChecker = task.NewTimeoutChecker(c.taskStore, timeoutCheckInterval, rebuildInterval)

	// 恢复孤儿任务：扫描终态但 ArchivedAt 未设置的任务（异常重启导致）
	c.recoverOrphanedTasks()

	// 创建并发任务信号量
	maxConcurrent := c.Config.GetInt("task.max_concurrent")
	if maxConcurrent <= 0 {
		maxConcurrent = 100
	}
	c.taskSem = make(chan struct{}, maxConcurrent)
	c.Logger.Info("Task concurrency limit", log.Int("max_concurrent", maxConcurrent))

	// 占满信号量以反映已有活跃任务
	// 仅统计非终态任务，避免已归档/终态任务占位
	activeTasks, err := c.taskStore.Count(task.TaskFilter{IncludeArchived: false})
	if err == nil {
		limit := maxConcurrent
		if activeTasks < limit {
			limit = activeTasks
		}
		for i := 0; i < limit; i++ {
			c.taskSem <- struct{}{}
		}
		c.Logger.Info("Active tasks restored to concurrency semaphore",
			log.Int("count", limit))
	}

	return nil
}

// recoverOrphanedTasks 恢复异常重启后滞留的任务（终态但未归档）
func (c *Core) recoverOrphanedTasks() {
	smConfig := c.stateMachineEngine.GetStateMachineConfig()
	tasks, err := c.taskStore.List(task.TaskFilter{})
	if err != nil {
		c.Logger.Error("Failed to list tasks for recovery", log.Error(err))
		return
	}
	var recovered int
	for _, t := range tasks {
		if smConfig.IsTerminalState(string(t.State)) && t.ArchivedAt == nil {
			now := time.Now()
			t.ArchivedAt = &now
			if err := c.taskStore.Update(t); err != nil {
				c.Logger.Error("Failed to recover orphaned task",
					log.String("task_id", t.ID),
					log.String("state", string(t.State)),
					log.Error(err))
				continue
			}
			c.Logger.Warn("Recovered orphaned task",
				log.String("task_id", t.ID),
				log.String("state", string(t.State)))
			recovered++
		}
	}
	if recovered > 0 {
		c.Logger.Warn("Orphaned task recovery complete",
			log.Int("recovered", recovered))
	}
}

// initArchiveCleaner 初始化归档清理器
func (c *Core) initArchiveCleaner() (*task.ArchiveCleaner, error) {
	retentionDays := c.Config.GetInt("task.archive.retention_days")
	if retentionDays <= 0 {
		return nil, nil
	}
	intervalStr := c.Config.GetString("task.archive.cleanup_interval")
	interval, _ := time.ParseDuration(intervalStr)
	if interval <= 0 {
		interval = time.Hour
	}
	cleaner := task.NewArchiveCleaner(c.taskStore, retentionDays, interval)
	cleaner.SetMetrics(c.Metrics)
	c.Logger.Info("Archive cleaner configured",
		log.Int("retention_days", retentionDays),
		log.String("interval", interval.String()))
	return cleaner, nil
}

// initDataSources 初始化数据源管理器
func (c *Core) initDataSources() error {
	// 创建事件channel
	bufferSize := c.Config.GetInt("datasource.event_channel_buffer_size")
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	eventCh := make(chan *common.Event, bufferSize)

	// 创建数据源管理器
	c.DataSourceManager = datasource.NewDataSourceManager(eventCh)
	c.DataSourceManager.SetLogger(c.Logger)

	// 从配置初始化数据源
	if c.Config != nil {
		c.Logger.Info("Initializing datasource from config")
		if err := c.DataSourceManager.Init(c.Config); err != nil {
			return fmt.Errorf("init datasource: %w", err)
		}
		c.Logger.Info("Datasource initialized")
	}

	// 保存 eventCh，initPolicyEngine 完成后启动消费
	c.eventCh = eventCh

	return nil
}

// startEventProcessor 启动事件处理 goroutine（在 PolicyEngine 初始化后调用）
func (c *Core) startEventProcessor() {
	if c.eventCh != nil {
		c.initEventLimiter()
		c.wg.Add(1)
		go c.processEvents(c.eventCh)
	}
}

// initEventLimiter 初始化事件速率限制
func (c *Core) initEventLimiter() {
	if c.Config != nil {
		rateVal := c.Config.GetInt("server.event_rate_limit")
		if rateVal <= 0 {
			rateVal = 10
		}
		burstVal := c.Config.GetInt("server.event_burst")
		if burstVal <= 0 {
			burstVal = 10
		}
		c.eventLimiter = rate.NewLimiter(rate.Limit(rateVal), burstVal)
	} else {
		c.eventLimiter = rate.NewLimiter(10, 10)
	}
}

// processEvents 处理数据源事件
// 策略匹配成功后通过 policyMatchedCh 直接 channel 通知任务调度创建任务
func (c *Core) processEvents(eventCh <-chan *common.Event) {
	defer func() {
		if r := recover(); r != nil {
			c.Logger.Error("panic in event processor", log.Any("recover", r))
		}
	}()
	defer c.wg.Done()
	c.Logger.Info("Event processor started")

	for {
		select {
		case <-c.ctx.Done():
			c.Logger.Info("Event processor stopped")
			return
		case event := <-eventCh:
			if event == nil {
				continue
			}

			// EventLog: 数据源事件采集
			if c.eventLog != nil {
				c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
					ID:        common.GenerateUUID(),
					TraceID:   event.TraceID,
					EventID:   event.ID,
					Stage:     eventlog.StageDataSource,
					EventType: event.Type,
					Source:    event.Source,
					Timestamp: event.Timestamp,
				})
			}

			// 速率限制
			if err := c.eventLimiter.Wait(c.ctx); err != nil {
				continue
			}

			// 创建追踪 span
			ctx := c.ctx
			var span oteltrace.Span
			if c.tracer != nil {
				ctx, span = c.tracer.Start(ctx, "event.process",
					oteltrace.WithAttributes(
						attribute.String("event.id", event.ID),
						attribute.String("event.type", event.Type),
						attribute.String("event.source", event.Source),
						attribute.String("event.trace_id", event.TraceID),
					),
				)
			}

			// 策略匹配
			matches, err := c.PolicyEngine.Match(ctx, event)
			if err != nil {
				c.Logger.Error("Policy match error", log.Error(err))
				if span != nil {
					span.End()
				}
				continue
			}

			// 处理匹配结果
			for _, match := range matches {
				if !match.Matched {
					continue
				}
				// EventLog: 策略匹配结果
				if c.eventLog != nil {
					c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
						ID:        common.GenerateUUID(),
						TraceID:   event.TraceID,
						EventID:   event.ID,
						Stage:     eventlog.StagePolicyMatch,
						EventType: "PolicyMatched",
						Source:    "policy-engine",
						Timestamp: time.Now(),
						Payload:   map[string]interface{}{"policy_id": match.PolicyID},
					})
				}
				c.processMatchedEvent(match, event)
			}

			if span != nil {
				span.End()
			}
		}
	}
}

// processMatchedEvent 处理策略匹配成功的事件
func (c *Core) processMatchedEvent(match *policy.PolicyMatch, sourceEvent *common.Event) {
	// 继承源事件的 ctx（含 TraceID），保持链路追踪连续性
	sourceCtx := c.ctx
	if sourceEvent != nil && sourceEvent.Ctx != nil {
		sourceCtx = sourceEvent.Ctx
	}

	// 创建 child span
	if c.tracer != nil {
		var span oteltrace.Span
		sourceCtx, span = c.tracer.Start(sourceCtx, "policy.matched",
			oteltrace.WithAttributes(
				attribute.String("policy.id", match.PolicyID),
			),
		)
		defer span.End()
	}

	matchedEvent := common.NewEvent(
		"PolicyMatched",
		"policy.matched",
		"policy-engine",
	).WithContext(sourceCtx)

	triggerEventType := ""
	triggerEventID := ""
	if sourceEvent != nil {
		matchedEvent.Payload = sourceEvent.Payload
		triggerEventType = sourceEvent.Type
		triggerEventID = sourceEvent.ID
	}

	if matchedEvent.Payload == nil {
		matchedEvent.Payload = make(map[string]interface{})
	}
	matchedEvent.Payload["policy_id"] = match.PolicyID
	if sourceEvent != nil {
		matchedEvent.Payload["trigger_event_type"] = triggerEventType
		matchedEvent.Payload["trigger_event_id"] = triggerEventID
	}
	matchedEvent.Payload["policy_timeout"] = match.Timeout

	extensions := make(map[string]string)
	for k, v := range match.Expansion {
		if s, ok := v.(string); ok {
			extensions[k] = s
		}
	}
	matchedEvent.TypedPayload = &api.Event_Policy{
		Policy: &api.PolicyEventPayload{
			PolicyId:         match.PolicyID,
			TriggerEventType: triggerEventType,
			TriggerEventId:   triggerEventID,
			Timestamp:        time.Now().Unix(),
			Extensions:       extensions,
		},
	}

	select {
	case c.policyMatchedCh <- matchedEvent:
	default:
		c.Logger.Warn("policyMatchedCh full, dropping event")
	}
}

// startTaskEventHandler 启动 PolicyMatchedEvent 处理器
// 通过直接 channel 接收 policy.matched 事件，创建任务并启动状态机
func (c *Core) startTaskEventHandler() {
	c.Logger.Info("Starting task event handler")
	c.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.Logger.Error("panic in task event handler", log.Any("recover", r))
			}
		}()
		defer c.wg.Done()
		c.Logger.Info("Task event handler started, waiting for PolicyMatchedEvent")

		for {
			select {
			case <-c.ctx.Done():
				c.Logger.Info("Task event handler stopped")
				return
			case event := <-c.policyMatchedCh:
				if event == nil {
					continue
				}
				c.handlePolicyMatchedEvent(event)
			}
		}
	}()
}

// startTransitionCommandHandler 启动 state.transition.command 处理器
// 外部组件（如 component-example）通过 EventBus 发布状态转换请求
func (c *Core) startTransitionCommandHandler() {
	c.Logger.Info("Starting transition command handler")
	c.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.Logger.Error("panic in transition command handler", log.Any("recover", r))
			}
		}()
		defer c.wg.Done()

		ch := c.EventBus.Subscribe(common.StateTransitionCommandTopic)
		for {
			select {
			case <-c.ctx.Done():
				c.Logger.Info("Transition command handler stopped")
				return
			case event, ok := <-ch:
				if !ok {
					c.Logger.Warn("Transition command channel closed, waiting for context cancellation")
					<-c.ctx.Done()
					return
				}
				if event == nil {
					continue
				}
				c.handleTransitionCommand(event)
			}
		}
	}()
}

// handleTransitionCommand 处理 state.transition.command 事件
// 必须使用 TypedPayload（protobuf）解析，ProtobufSerializer 不序列化 Payload map
func (c *Core) handleTransitionCommand(event *common.Event) {
	if event.TypedPayload == nil {
		c.Logger.Warn("Invalid transition command: missing TypedPayload")
		return
	}

	comp, ok := event.TypedPayload.(*api.Event_Component)
	if !ok || comp.Component == nil {
		c.Logger.Warn("Invalid transition command: unexpected payload type")
		return
	}

	taskID := comp.Component.TaskId
	currentState := comp.Component.CurrentState
	targetState := comp.Component.TargetState
	componentName := comp.Component.ComponentName
	success := comp.Component.Success
	message := comp.Component.Message

	if taskID == "" || currentState == "" || targetState == "" {
		c.Logger.Warn("Invalid transition command: missing required fields")
		return
	}

	// 创建 child span
	ctx := c.ctx
	if c.tracer != nil {
		var span oteltrace.Span
		ctx, span = c.tracer.Start(event.Ctx, "transition.command",
			oteltrace.WithAttributes(
				attribute.String("task.id", taskID),
				attribute.String("transition.from", currentState),
				attribute.String("transition.to", targetState),
			),
		)
		defer span.End()
	}

	c.Logger.Info("Received transition command",
		log.String("task_id", taskID),
		log.String("from", currentState),
		log.String("to", targetState),
		log.String("component", componentName),
		log.Any("success", success))

	// EventLog: 状态转换命令接收
	if c.eventLog != nil {
		c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
			ID:            common.GenerateUUID(),
			TraceID:       event.TraceID,
			Stage:         eventlog.StageCommand,
			EventType:     "StateTransitionCommand",
			Source:        componentName,
			Timestamp:     time.Now(),
			TaskID:        taskID,
			OldState:      currentState,
			NewState:      targetState,
			ComponentName: componentName,
			Success:       &success,
			Message:       message,
		})
	}

	cmd := task.TransitionCommand{
		TaskID:       taskID,
		CurrentState: task.TaskState(currentState),
		TargetState:  task.TaskState(targetState),
		ComponentInfo: task.ComponentInfo{
			Name: componentName,
		},
		Result: &task.CommandResult{
			Success: success,
			Message: message,
		},
	}

	if err := c.stateMachineEngine.HandleTransitionCommand(ctx, cmd); err != nil {
		c.Logger.Error("Failed to handle transition command",
			log.String("task_id", taskID),
			log.String("from", currentState),
			log.String("to", targetState),
			log.Error(err))
		return
	}

	// 转换到终态时释放并发槽位
	smCfg := c.stateMachineEngine.GetStateMachineConfig()
	if smCfg.IsTerminalState(targetState) {
		select {
		case <-c.taskSem:
		default:
		}
	}
}

// handlePolicyMatchedEvent 处理 PolicyMatchedEvent，创建任务并启动状态机
func (c *Core) handlePolicyMatchedEvent(event *common.Event) {
	// 获取并发槽位（达到上限时阻塞，天然反压至数据源）
	select {
	case c.taskSem <- struct{}{}:
	case <-c.ctx.Done():
		return
	}

	// 创建 child span
	ctx := c.ctx
	if c.tracer != nil {
		var span oteltrace.Span
		ctx, span = c.tracer.Start(event.Ctx, "task.create")
		defer span.End()
	}

	// 解析事件
	policyID := event.GetPayloadString("policy_id")
	triggerEventType := event.GetPayloadString("trigger_event_type")
	triggerEventID := event.GetPayloadString("trigger_event_id")

	// 生成任务ID（使用UUID保证唯一性）
	taskID := common.GenerateUUID()

	// 构建 metadata
	metadata := map[string]string{
		"policy_id":          policyID,
		"trigger_event_type": triggerEventType,
		"trigger_event_id":   triggerEventID,
	}
	// 持久化 rule timeout，handleTaskRetry 时用于重新计算生效超时
	if timeout := event.GetPayloadString("policy_timeout"); timeout != "" {
		metadata["policy_timeout"] = timeout
	}
	// 传递 TraceID，CreateTask 时写入 Task.TraceID
	if event.TraceID != "" {
		metadata["trace_id"] = event.TraceID
	}
	// 将策略的 Expansion 内容存储到任务的 metadata 中（转换为字符串）
	if tp, ok := event.TypedPayload.(*api.Event_Policy); ok && tp.Policy != nil {
		for key, value := range tp.Policy.Extensions {
			metadata[key] = value
		}
	}

	// 使用 StateMachineEngine 创建任务
	spec := task.TaskSpec{
		ID:          taskID,
		Name:        fmt.Sprintf("Task for policy %s", policyID),
		Description: fmt.Sprintf("Policy %s triggered by %s", policyID, triggerEventType),
		Priority:    0,
		Metadata:    metadata,
		Event:       event,
	}

	t, err := c.stateMachineEngine.CreateTask(ctx, spec)
	if err != nil {
		c.Logger.Error("Failed to create task via state machine engine", log.Error(err))
		<-c.taskSem
		return
	}

	// EventLog: 任务创建
	if c.eventLog != nil {
		c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
			ID:        common.GenerateUUID(),
			TraceID:   event.TraceID,
			Stage:     eventlog.StageTaskCreate,
			EventType: "TaskCreated",
			Source:    "statemachine-engine",
			Timestamp: time.Now(),
			TaskID:    t.ID,
			NewState:  string(t.State),
		})
	}

	// 计算有效超时并设 TimeoutAt
	effective := computeEffectiveTimeout(
		event.GetPayloadString("policy_timeout"),
		c.Config.GetString("task.default_timeout"),
	)

	t.TimeoutAt = new(time.Time)
	*t.TimeoutAt = time.Now().Add(effective)
	if err := c.taskStore.Update(t); err != nil {
		c.Logger.Error("Failed to set task timeout", log.String("task_id", t.ID), log.Error(err))
	} else {
		c.timeoutChecker.Push(t.ID, *t.TimeoutAt)
	}

	// 发布任务状态变更事件 (使用配置中的初始状态)
	topic := fmt.Sprintf("%s%s", common.TaskEventTopicPrefix, string(t.State))

	stateEvent := common.NewEvent(
		"TaskStateChanged",
		topic,
		"statemachine-engine",
	).WithContext(event.Ctx)
	// 构建强类型 payload
	extensions := make(map[string]string)
	for key, value := range t.Metadata {
		if key != "policy_id" && key != "trigger_event_type" && key != "trigger_event_id" {
			extensions[key] = value
		}
	}
	stateEvent.TypedPayload = &api.Event_Task{
		Task: &api.TaskEventPayload{
			TaskId:           t.ID,
			OldState:         "",
			NewState:         string(t.State),
			PolicyId:         policyID,
			TriggerEventType: metadata["trigger_event_type"],
			TriggerEventId:   metadata["trigger_event_id"],
			Extensions:       extensions,
		},
	}
	// EventLog: 任务状态变更（创建时）
	if c.eventLog != nil {
		c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
			ID:        common.GenerateUUID(),
			TraceID:   event.TraceID,
			Stage:     eventlog.StageTaskState,
			EventType: "TaskStateChanged",
			Topic:     topic,
			Source:    "statemachine-engine",
			Timestamp: time.Now(),
			TaskID:    t.ID,
			NewState:  string(t.State),
		})
	}

	if err := c.EventBus.Publish(topic, stateEvent); err != nil {
		c.Logger.Error("Failed to publish task state changed", log.Error(err))
	}
}

// handleTimeoutEvent 处理超时事件
func (c *Core) handleTimeoutEvent(ctx context.Context, taskID string, currentState task.TaskState) {
	if taskID == "" || currentState == "" {
		c.Logger.Warn("Invalid timeout event: missing task_id or current_state")
		return
	}

	// 获取任务
	tsk, err := c.taskStore.Get(taskID)
	if err != nil {
		c.Logger.Error("Timeout handler: task not found", log.String("task_id", taskID), log.Error(err))
		return
	}

	// 状态已变更，跳过（任务可能已被其他组件处理）
	if tsk.State != currentState {
		c.Logger.Info("Timeout handler: state changed since timeout, skipping",
			log.String("task_id", taskID),
			log.String("expected", string(currentState)),
			log.String("actual", string(tsk.State)))
		return
	}

	// 获取状态配置
	cfg := c.stateMachineEngine.GetStateMachineConfig()
	stateCfg, ok := cfg.States[string(tsk.State)]
	if !ok {
		c.Logger.Warn("Timeout handler: state config not found",
			log.String("task_id", taskID),
			log.String("state", string(tsk.State)))
		return
	}

	// EventLog: 超时处理
	if c.eventLog != nil {
		c.eventLog.Append(c.ctx, &eventlog.EventLogEntry{
			ID:        common.GenerateUUID(),
			TraceID:   tsk.TraceID,
			Stage:     eventlog.StageTimeout,
			EventType: "TaskTimeout",
			Source:    "timeout-handler",
			Timestamp: time.Now(),
			TaskID:    taskID,
			OldState:  string(currentState),
			Message:   fmt.Sprintf("timeout, auto_retry=%v", stateCfg.AutoRetry),
		})
	}

	if stateCfg.AutoRetry && tsk.RetryCount < stateCfg.MaxRetries {
		c.handleTaskRetry(ctx, tsk, cfg, stateCfg)
	} else {
		c.handleTaskArchive(ctx, tsk, cfg)
	}
}

// computeEffectiveTimeout 计算生效超时
// ruleTimeoutStr: rule timeout（Go duration 格式），空串/无效视为未设置
// defaultTimeoutStr: 配置兜底（Go duration 格式），空串/无效用 30 分钟
func computeEffectiveTimeout(ruleTimeoutStr, defaultTimeoutStr string) time.Duration {
	defaultTimeout, _ := time.ParseDuration(defaultTimeoutStr)
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Minute
	}
	ruleTimeout, err := time.ParseDuration(ruleTimeoutStr)
	if err == nil && ruleTimeout > 0 && ruleTimeout < defaultTimeout {
		return ruleTimeout
	}
	return defaultTimeout
}

// backoff 计算指数退避延迟：1s, 2s, 4s, 8s, ... 最大 maxBackoffSeconds 秒
func (c *Core) backoff(retryCount int) time.Duration {
	maxBackoff := c.Config.GetInt("task.retry.max_backoff_seconds")
	if maxBackoff <= 0 {
		maxBackoff = 60
	}
	n := 1 << uint(retryCount)
	if n > maxBackoff {
		n = maxBackoff
	}
	return time.Duration(n) * time.Second
}

// handleTaskRetry 处理超时重试（含指数退避）
func (c *Core) handleTaskRetry(ctx context.Context, tsk *task.Task, cfg *task.StateMachineConfig, stateCfg task.StateConfig) {
	// 指数退避：如果还没到退避时间，跳过此 tick
	if tsk.StateUpdatedAt.Add(c.backoff(tsk.RetryCount)).After(time.Now()) {
		c.Logger.Info("Task retry backoff not yet elapsed, skipping",
			log.String("task_id", tsk.ID),
			log.Int("retry_count", tsk.RetryCount),
			log.String("backoff", c.backoff(tsk.RetryCount).String()))
		return
	}

	// 确定目标状态
	targetState := stateCfg.RetryToState
	if targetState == "" {
		targetState = cfg.InitialState
	}

	// 新重试次数（原子化写入，与状态转换一起提交）
	newRetryCount := tsk.RetryCount + 1

	// 计算新生效超时
	effective := computeEffectiveTimeout(tsk.Metadata["policy_timeout"],
		c.Config.GetString("task.default_timeout"))
	var newTimeout *time.Time
	if effective > 0 {
		newTimeout = new(time.Time)
		*newTimeout = time.Now().Add(effective)
	}

	// 转换到重试状态（原子化更新 TimeoutAt）
	cmd := task.TransitionCommand{
		TaskID:       tsk.ID,
		CurrentState: tsk.State,
		TargetState:  task.TaskState(targetState),
		ComponentInfo: task.ComponentInfo{
			Name: "timeout-handler",
		},
		Result: &task.CommandResult{
			Success: true,
			Message: fmt.Sprintf("retry #%d after timeout", newRetryCount),
		},
		SetRetryCount: &newRetryCount,
		SetTimeoutAt:  newTimeout,
	}

	if err := c.stateMachineEngine.HandleTransitionCommand(ctx, cmd); err != nil {
		c.Logger.Error("Failed to transition task to retry state",
			log.String("task_id", tsk.ID),
			log.String("target", targetState),
			log.Error(err))
		return
	}

	if newTimeout != nil {
		c.timeoutChecker.Push(tsk.ID, *newTimeout)
	}
}

// handleTaskArchive 处理超时归档
func (c *Core) handleTaskArchive(ctx context.Context, tsk *task.Task, cfg *task.StateMachineConfig) {
	// 从配置读取所有终态，逐个尝试转换
	var transitioned bool
	for _, target := range cfg.TerminalStates {
		if cfg.IsTransitionAllowed(string(tsk.State), target) {
			cmd := task.TransitionCommand{
				TaskID:       tsk.ID,
				CurrentState: tsk.State,
				TargetState:  task.TaskState(target),
				ComponentInfo: task.ComponentInfo{Name: "timeout-handler"},
				Result: &task.CommandResult{
					Success: false,
					Message: fmt.Sprintf("timeout in state %s after %d retries", tsk.State, tsk.RetryCount),
				},
			}
			if err := c.stateMachineEngine.HandleTransitionCommand(ctx, cmd); err != nil {
				c.Logger.Warn("Failed to transition timed-out task to terminal state",
					log.String("task_id", tsk.ID),
					log.String("target", target),
					log.Error(err))
				continue
			}
			transitioned = true
			break
		}
	}

	// 状态转换成功后写 Result
	if transitioned {
		result := &task.TaskResult{
			Success: false,
			Error:   fmt.Sprintf("task timed out in state %s after %d retries", tsk.State, tsk.RetryCount),
		}
		if err := c.taskStore.UpdateResult(tsk.ID, result); err != nil {
			c.Logger.Error("Failed to update task result on timeout", log.String("task_id", tsk.ID), log.Error(err))
		}
		// 释放并发槽位
		select {
		case <-c.taskSem:
		default:
		}
	}

	if !transitioned {
		c.Logger.Error("No valid terminal transition for timed-out task, task may be stuck",
			log.String("task_id", tsk.ID),
			log.String("state", string(tsk.State)))
	}
}

// initPolicyEngine 初始化策略引擎
// core 只创建管理器和存储，具体策略和 DSL 引擎由 policy 模块内部根据配置创建
func (c *Core) initPolicyEngine() error {
	var store policy.PolicyStore

	// 根据配置创建策略存储
	if c.Config != nil && c.Config.GetMap("policy.db") != nil {
		policyDBType := c.Config.GetString("policy.db.type")
		if policyDBType == "" {
			policyDBType = "memory"
		}
		policyDB, err := db.DefaultFactory.Create(db.Config{
			Type:      policyDBType,
			Path:      c.Config.GetString("policy.db.path"),
			TableName: "policies",
		})
		if err == nil {
			store = policy.NewPolicyStore(policyDB)
			c.Logger.Info("Policy store created", log.String("type", policyDBType))
		} else {
			c.Logger.Warn("Failed to create policy db, falling back to memory", log.Error(err))
		}
	}

	if store == nil {
		// 使用内存存储作为默认
		store = policy.NewMemoryPolicyStore()
		c.Logger.Info("Policy store created", log.String("type", "memory"))
	}

	// 创建策略管理器
	manager := policy.NewDefaultPolicyManager(store)

	// 创建策略引擎
	engine := policy.NewDefaultPolicyEngine(manager, store)
	engine.SetLogger(c.Logger)

	c.PolicyEngine = engine

	// 从配置初始化策略引擎
	if c.Config != nil {
		c.Logger.Info("Initializing policy engine from config")
		if err := c.PolicyEngine.Init(c.Config); err != nil {
			return fmt.Errorf("init policy engine: %w", err)
		}
		c.Logger.Info("Policy engine initialized")
	}

	return nil
}

// Start 启动Core
func (c *Core) Start() error {
	if !c.started.CompareAndSwap(false, true) {
		c.Logger.Warn("Core already started, ignoring duplicate Start")
		return nil
	}
	c.Logger.Info("Starting Core...")

	// 启动EventBus
	c.Logger.Info("Starting EventBus")
	if err := c.EventBus.Start(c.ctx); err != nil {
		return fmt.Errorf("start eventbus: %w", err)
	}
	c.Logger.Info("EventBus started")

	// 启动 PolicyMatchedEvent 处理器（通过 policyMatchedCh 直接 channel 接收，创建任务并启动状态机）
	c.Logger.Info("Starting PolicyMatchedEvent handler")
	c.startTaskEventHandler()
	c.Logger.Info("PolicyMatchedEvent handler started")

	// 启动 state.transition.command 处理器（处理外部组件的状态转换请求）
	c.Logger.Info("Starting transition command handler")
	c.startTransitionCommandHandler()
	c.Logger.Info("Transition command handler started")

	// 设置超时回调（必须在 Start 之前设置，防止竞态）
	c.timeoutChecker.SetOnTimeout(func(taskID string, currentState task.TaskState) {
		c.handleTimeoutEvent(c.ctx, taskID, currentState)
	})

	// 启动超时检查器
	c.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.Logger.Error("panic in timeout checker", log.Any("recover", r))
			}
		}()
		defer c.wg.Done()
		if err := c.timeoutChecker.Start(c.ctx); err != nil {
			c.Logger.Error("TimeoutChecker error", log.Error(err))
		}
	}()

	// 启动策略引擎
	c.Logger.Info("Starting policy engine")
	if err := c.PolicyEngine.Start(c.ctx); err != nil {
		return fmt.Errorf("start policy engine: %w", err)
	}
	c.Logger.Info("Policy engine started")

	// 启动事件处理（PolicyEngine 就绪后，DataSourceManager 启动前）
	c.startEventProcessor()
	c.Logger.Info("Event processor started")

	// 启动数据源
	c.Logger.Info("Starting datasource")
	if err := c.DataSourceManager.Start(c.ctx); err != nil {
		return fmt.Errorf("start datasource: %w", err)
	}
	c.Logger.Info("Datasource started")

	// 启动HTTP服务器
	c.Logger.Info("Starting HTTP server")
	if err := c.startHTTPServer(); err != nil {
		return fmt.Errorf("start http server: %w", err)
	}
	c.Logger.Info("HTTP server started")

	// 启动归档清理器
	if c.archiveCleaner != nil {
		c.Logger.Info("Starting archive cleaner")
		c.archiveCleaner.Start(c.ctx)
		c.Logger.Info("Archive cleaner started")
	}

	c.Logger.Info("Core started successfully")
	return nil
}

// Stop 停止Core
func (c *Core) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		c.Logger.Warn("Core already stopped, ignoring duplicate Stop")
		return nil
	}
	c.Logger.Info("Stopping Core...")

	// 先取消上下文，通知所有 goroutine 退出
	c.cancel()
	c.Logger.Info("Context cancelled")

	// 停止EventBus（关闭所有订阅 channel，让订阅 goroutine 退出）
	if c.EventBus != nil {
		c.Logger.Info("Stopping EventBus")
		if err := c.EventBus.Stop(); err != nil {
			c.Logger.Error("EventBus stop error", log.Error(err))
		}
		c.Logger.Info("EventBus stopped")
	}

	// 停止 Tracer（刷新缓冲区）
	if c.tracerProvider != nil {
		c.Logger.Info("Stopping tracer")
		tracerTimeout := c.Config.GetInt("shutdown.tracer_timeout")
		if tracerTimeout <= 0 {
			tracerTimeout = 5
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Duration(tracerTimeout)*time.Second)
		if err := c.tracerProvider.Shutdown(shutdownCtx); err != nil {
			c.Logger.Error("Tracer shutdown error", log.Error(err))
		}
		shutdownCancel()
		c.Logger.Info("Tracer stopped")
	}

	// 停止HTTP服务器（让 HTTP goroutine 先退出，避免阻塞 wg.Wait）
	if c.HTTPServer != nil {
		c.Logger.Info("Stopping HTTP server")
		httpTimeout := c.Config.GetInt("shutdown.http_timeout")
		if httpTimeout <= 0 {
			httpTimeout = 5
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Duration(httpTimeout)*time.Second)
		if err := c.HTTPServer.Shutdown(shutdownCtx); err != nil {
			c.Logger.Error("Failed to shutdown HTTP server", log.Error(err))
		}
		shutdownCancel()
		c.Logger.Info("HTTP shutdown completed")
	}

	// 等待所有 goroutine 退出（添加超时）
	c.Logger.Info("Waiting for goroutines to exit")

	gracePeriod := c.Config.GetInt("shutdown.grace_period")
	if gracePeriod <= 0 {
		gracePeriod = 30
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.Logger.Info("All goroutines exited")
	case <-time.After(time.Duration(gracePeriod) * time.Second):
		c.Logger.Warn("Timeout waiting for goroutines, forcing exit", log.Int("grace_period_sec", gracePeriod))
	}

	// 停止数据源
	if c.DataSourceManager != nil {
		c.Logger.Info("Stopping DataSourceManager")
		if err := c.DataSourceManager.Close(); err != nil {
			c.Logger.Error("DataSourceManager close error", log.Error(err))
		}
		c.Logger.Info("DataSourceManager stopped")
	}

	// 停止策略引擎
	if c.PolicyEngine != nil {
		c.Logger.Info("Stopping PolicyEngine")
		if err := c.PolicyEngine.Stop(); err != nil {
			c.Logger.Error("PolicyEngine stop error", log.Error(err))
		}
		c.Logger.Info("PolicyEngine stopped")
	}

	// 停止归档清理器
	if c.archiveCleaner != nil {
		c.Logger.Info("Stopping archive cleaner")
		c.archiveCleaner.Stop()
		c.Logger.Info("Archive cleaner stopped")
	}

	// 停止事件日志
	if c.eventLog != nil {
		c.Logger.Info("Stopping EventLog")
		if err := c.eventLog.Close(); err != nil {
			c.Logger.Error("EventLog close error", log.Error(err))
		}
		c.Logger.Info("EventLog stopped")
	}

	c.Logger.Info("Core stopped")
	return nil
}

// Health 健康检查
func (c *Core) Health() error {
	if c.taskStore == nil {
		return fmt.Errorf("task store not initialized")
	}

	if c.stateMachineEngine == nil {
		return fmt.Errorf("state machine engine not initialized")
	}

	if c.timeoutChecker == nil {
		return fmt.Errorf("timeout checker not initialized")
	}

	if c.EventBus != nil {
		if err := c.EventBus.Health(); err != nil {
			return fmt.Errorf("eventbus: %w", err)
		}
	}

	if c.PolicyEngine != nil {
		if err := c.PolicyEngine.Health(); err != nil {
			return fmt.Errorf("policy engine: %w", err)
		}
	}

	return nil
}

// startHTTPServer 启动HTTP服务器
func (c *Core) startHTTPServer() error {
	// 从配置读取地址，默认 tcp://0.0.0.0:8080
	address := "tcp://0.0.0.0:8080"
	if c.Config != nil {
		if addr := c.Config.GetString("server.address"); addr != "" {
			address = addr
		}
	}
	c.serverAddr = address

	// 认证中间件：优先使用 NUTS_AUTH_TOKEN 环境变量，否则自动生成随机 token
	authMiddleware := func(next http.Handler) http.Handler {
		token := os.Getenv("NUTS_AUTH_TOKEN")
		if token == "" {
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				c.Logger.Error("Failed to generate random auth token, using zero token", log.Error(err))
			} else {
				token = hex.EncodeToString(b)
			}
			c.Logger.Debug("API auth token generated", log.String("token", token))
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				c.writeJSON(w, common.Error(int(common.CodeUnauthorized), "invalid or missing auth token"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// 创建路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		c.handleStatus(w)
	})

	// 数据源API - 使用 datasource.HTTPHandler
	if c.DataSourceManager != nil {
		dsHandler := datasource.NewHTTPHandler(c.DataSourceManager)
		dsHandler.RegisterRoutes(mux)
	}

	// 策略API - 使用 policy.HTTPHandler
	if c.PolicyEngine != nil {
		policyHandler := policy.NewHTTPHandler(c.PolicyEngine.GetManager(), c.PolicyEngine)
		policyHandler.RegisterRoutesToMux(mux)
	}

	// 任务API
	mux.HandleFunc("/api/v1/tasks", c.handleTasks)
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/api/v1/tasks/"):]
		if strings.Contains(path, "/history") {
			c.handleTaskHistory(w, r)
		} else if strings.Contains(path, "/events") {
			c.handleTaskEvents(w, r)
		} else {
			c.handleTaskDetail(w, r)
		}
	})

	// 追踪API
	mux.HandleFunc("/api/v1/traces/", c.handleTraces)

	// 状态机API
	mux.HandleFunc("/api/v1/statemachine/config", c.handleStateMachineConfig)

	// 运行时调试 API
	mux.HandleFunc("/api/v1/debug/vars", c.handleDebugVars)

	// Prometheus 指标端点
	if c.prometheusEnabled {
		mux.Handle("/metrics", promhttp.Handler())
	}

	handler := loggingMiddleware(c.Logger)(authMiddleware(mux))
	if c.tracerProvider != nil {
		handler = otelhttp.NewHandler(handler, "nuts-http")
	}
	c.HTTPServer = &http.Server{
		Handler: handler,
	}

	// 根据地址格式创建 listener，失败则返回 error 而非静默降级
	lis, err := common.NewListener(c.serverAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", c.serverAddr, err)
	}

	c.Logger.Info("Starting HTTP server", log.String("addr", c.serverAddr))
	c.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.Logger.Error("panic in HTTP server", log.Any("recover", r))
			}
		}()
		defer c.wg.Done()
		if err = c.HTTPServer.Serve(lis); err != nil && err != http.ErrServerClosed {
			c.Logger.Error("HTTP server error", log.Error(err))
		}
		c.Logger.Info("HTTP server goroutine exited")
	}()

	c.Logger.Info("HTTP server goroutine started", log.String("addr", c.serverAddr))
	return nil
}

// handleStatus 处理状态请求
func (c *Core) handleStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	resp := common.Success(map[string]interface{}{
		"status":  "running",
		"version": "0.1.0",
		"address": c.serverAddr,
	})
	c.writeJSON(w, resp)
}

// handleTasks 处理任务列表请求
// 默认只返回活跃任务（未归档的），如需查看所有任务需传入 ?include_completed=true
func (c *Core) handleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.taskStore == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "task store not initialized")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	includeCompleted := r.URL.Query().Get("include_completed") == "true"
	traceID := r.URL.Query().Get("trace_id")

	filter := task.TaskFilter{IncludeArchived: includeCompleted}
	tasks, err := c.taskStore.List(filter)

	// 按 trace_id 过滤
	if traceID != "" && err == nil {
		filtered := make([]*task.Task, 0)
		for _, t := range tasks {
			if t.TraceID == traceID {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}
	if err != nil {
		resp := common.ErrorWithCode(common.CodeInternalError, err.Error())
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
		c.writeJSON(w, resp)
		return
	}

	resp := common.Success(map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
	c.writeJSON(w, resp)
}

// handleTaskDetail 处理任务详情请求
func (c *Core) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.taskStore == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "task store not initialized")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	id := r.URL.Path[len("/api/v1/tasks/"):]
	task, err := c.taskStore.Get(id)
	if err != nil {
		resp := common.Error(404, "task not found")
		c.writeJSON(w, resp)
		return
	}

	resp := common.Success(task)
	c.writeJSON(w, resp)
}

// handleTaskHistory 处理任务历史请求
func (c *Core) handleTaskHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.stateMachineEngine == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "state machine engine not initialized")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	// 解析路径: /api/v1/tasks/:id/history
	path := r.URL.Path[len("/api/v1/tasks/"):]
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "history" {
		resp := common.Error(400, "invalid path")
		c.writeJSON(w, resp)
		return
	}
	taskID := parts[0]

	history, err := c.stateMachineEngine.GetTaskHistory(taskID)
	if err != nil {
		resp := common.Error(404, fmt.Sprintf("task history not found: %v", err))
		c.writeJSON(w, resp)
		return
	}

	resp := common.Success(history)
	c.writeJSON(w, resp)
}

// handleTaskEvents 处理任务事件日志请求
func (c *Core) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.eventLog == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "eventlog not enabled")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	// 解析路径: /api/v1/tasks/:id/events
	path := r.URL.Path[len("/api/v1/tasks/"):]
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "events" {
		resp := common.Error(400, "invalid path")
		c.writeJSON(w, resp)
		return
	}
	taskID := parts[0]

	entries, err := c.eventLog.QueryByTaskID(taskID)
	if err != nil {
		resp := common.Error(500, fmt.Sprintf("query eventlog: %v", err))
		c.writeJSON(w, resp)
		return
	}

	resp := common.Success(map[string]interface{}{
		"task_id": taskID,
		"events":  entries,
		"count":   len(entries),
	})
	c.writeJSON(w, resp)
}

// handleTraces 处理追踪查询请求
func (c *Core) handleTraces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.eventLog == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "eventlog not enabled")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	// 解析路径: /api/v1/traces/:trace_id
	traceID := r.URL.Path[len("/api/v1/traces/"):]
	if traceID == "" {
		resp := common.Error(400, "trace_id is required")
		c.writeJSON(w, resp)
		return
	}

	timeline, err := c.eventLog.QueryByTraceID(traceID)
	if err != nil {
		resp := common.Error(500, fmt.Sprintf("query trace: %v", err))
		c.writeJSON(w, resp)
		return
	}

	resp := common.Success(timeline)
	c.writeJSON(w, resp)
}

// handleStateMachineConfig 处理状态机配置请求
func (c *Core) handleStateMachineConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.stateMachineEngine == nil {
		resp := common.ErrorWithCode(common.CodeServiceUnavailable, "state machine engine not initialized")
		w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeServiceUnavailable))
		c.writeJSON(w, resp)
		return
	}

	config := c.stateMachineEngine.GetStateMachineConfig()
	resp := common.Success(config)
	c.writeJSON(w, resp)
}

// handleDebugVars 暴露运行时状态，用于运维排障
func (c *Core) handleDebugVars(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	vars := map[string]interface{}{
		"goroutines": runtime.NumGoroutine(),
		"num_cpu":    runtime.NumCPU(),
		"go_version": runtime.Version(),
		"memory": map[string]interface{}{
			"alloc_bytes":       memStats.Alloc,
			"total_alloc_bytes": memStats.TotalAlloc,
			"sys_bytes":        memStats.Sys,
			"heap_alloc_bytes": memStats.HeapAlloc,
			"heap_sys_bytes":   memStats.HeapSys,
			"gc_cycles":        memStats.NumGC,
		},
		"started": c.started.Load(),
		"stopped": c.stopped.Load(),
	}

	// 任务队列深度（按状态统计，状态列表从配置获取）
	if c.taskStore != nil && c.stateMachineEngine != nil {
		smConfig := c.stateMachineEngine.GetStateMachineConfig()
		queueDepth := make(map[string]int)
		var activeTotal int
		for state := range smConfig.States {
			count, err := c.taskStore.Count(task.TaskFilter{State: task.TaskState(state)})
			if err == nil {
				queueDepth[state] = count
				activeTotal += count
			}
		}
		queueDepth["active_total"] = activeTotal
		allWithArchived, _ := c.taskStore.Count(task.TaskFilter{IncludeArchived: true})
		queueDepth["archived_total"] = allWithArchived - activeTotal
		vars["task_queue"] = queueDepth
	}

	// EventBus 状态
	type eventbusInfo struct {
		HasSubscribers bool `json:"has_subscribers"`
	}
	if grpcBus, ok := c.EventBus.(*eventbus.GRPCEventBus); ok {
		info := eventbusInfo{
			HasSubscribers: grpcBus.HasAnyRemoteSubscribers(),
		}
		vars["eventbus"] = info
	}

	c.writeJSON(w, vars)
}

// writeJSON safely encodes a JSON response, logging any encoding errors.
func (c *Core) writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		c.Logger.Error("Failed to write JSON response", log.Error(err))
	}
}
